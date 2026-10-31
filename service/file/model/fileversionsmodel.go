package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ FileVersionsModel = (*customFileVersionsModel)(nil)

// ErrPermissionDenied 由 AppendVersion 返回，表示文件不属于该用户。
var ErrPermissionDenied = errors.New("permission denied")

type (
	// FileVersionsModel is an interface to be customized, add more methods here,
	// and implement the added methods in customFileVersionsModel.
	FileVersionsModel interface {
		fileVersionsModel
		// FindByFileId 返回某个文件的全部版本，按版本号倒序。
		FindByFileId(ctx context.Context, fileId uint64) ([]*FileVersions, error)
		// AppendVersion 在事务内追加一个新版本并把文件指针指向它。
		// 幂等：同一个 request_id 重复调用返回已存在的版本，changed=false。
		AppendVersion(ctx context.Context, in AppendVersionInput) (version uint64, changed bool, err error)
	}

	customFileVersionsModel struct {
		*defaultFileVersionsModel
	}
)

// AppendVersionInput 是追加版本所需的全部字段。
type AppendVersionInput struct {
	FileId      uint64
	OwnerId     uint64
	RequestId   string
	Size        uint64
	ContentType string
	ContentHash string
	ObjectKey   string
}

// NewFileVersionsModel returns a model for the database table.
func NewFileVersionsModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) FileVersionsModel {
	return &customFileVersionsModel{
		defaultFileVersionsModel: newFileVersionsModel(conn, c, opts...),
	}
}

// FindByFileId 按版本号倒序返回全部版本。
func (m *customFileVersionsModel) FindByFileId(ctx context.Context, fileId uint64) ([]*FileVersions, error) {
	var versions []*FileVersions
	err := m.QueryRowsNoCacheCtx(ctx, &versions,
		"select `id`,`file_id`,`owner_id`,`version`,`size`,`content_type`,`content_hash`,"+
			"`object_key`,`request_id`,`created_at` from `file_versions` "+
			"where `file_id` = ? order by `version` desc",
		fileId)
	if err != nil {
		return nil, err
	}
	return versions, nil
}

// versionState 是事务内加锁读取的最小字段集合。
type versionState struct {
	Version uint64 `db:"version"`
	OwnerId uint64 `db:"owner_id"`
}

// AppendVersion 用「行锁 + 单事务」保证并发下版本号不冲突：
//   - SELECT ... FOR UPDATE 锁住 files 行，读当前版本号；
//   - 同一个 request_id 已经落地过则直接返回（幂等）；
//   - 插入新版本行并更新 files 的当前内容指针，两步在同一事务内。
func (m *customFileVersionsModel) AppendVersion(ctx context.Context,
	in AppendVersionInput) (uint64, bool, error) {
	var (
		version uint64
		changed bool
	)

	err := m.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		var state versionState
		err := session.QueryRowCtx(ctx, &state,
			"select `version`, `owner_id` from `files` where `id` = ? and `deleted_at` is null for update",
			in.FileId)
		if errors.Is(err, sqlx.ErrNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if state.OwnerId != in.OwnerId {
			return ErrPermissionDenied
		}

		// 幂等键命中：这次请求已经生效过，直接返回已有版本。
		var existing struct {
			Version uint64 `db:"version"`
		}
		err = session.QueryRowCtx(ctx, &existing,
			"select `version` from `file_versions` where `file_id` = ? and `request_id` = ?",
			in.FileId, sql.NullString{String: in.RequestId, Valid: in.RequestId != ""})
		if err == nil {
			version = existing.Version
			changed = false
			return nil
		}
		if !errors.Is(err, sqlx.ErrNotFound) {
			return err
		}

		version = state.Version + 1
		_, err = session.ExecCtx(ctx,
			"insert into `file_versions` (`file_id`,`owner_id`,`version`,`size`,`content_type`,"+
				"`content_hash`,`object_key`,`request_id`) values (?,?,?,?,?,?,?,?)",
			in.FileId, in.OwnerId, version, in.Size, in.ContentType, in.ContentHash,
			in.ObjectKey, sql.NullString{String: in.RequestId, Valid: in.RequestId != ""})
		if err != nil {
			return err
		}

		_, err = session.ExecCtx(ctx,
			"update `files` set `size` = ?, `content_type` = ?, `content_hash` = ?, "+
				"`object_key` = ?, `version` = ?, `status` = 'available' where `id` = ?",
			in.Size, in.ContentType, in.ContentHash, in.ObjectKey, version, in.FileId)
		if err != nil {
			return err
		}

		changed = true
		return nil
	})
	if err != nil {
		return 0, false, err
	}

	// 事务提交后再失效缓存，避免把未提交的数据写回缓存。
	if changed {
		if err := m.DelCacheCtx(ctx, fmt.Sprintf("%s%v", cacheFilesIdPrefix, in.FileId)); err != nil {
			return version, changed, nil
		}
	}
	return version, changed, nil
}
