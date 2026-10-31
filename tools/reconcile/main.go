// Command reconcile 对账：检查 MySQL 元数据与 MinIO 对象是否一致。
//
// 三件事：
//  1. 数据库里 status=available 的对象，在 MinIO 是否真的存在；
//  2. MinIO 的 u/ 前缀下有没有数据库不认识的孤儿对象；
//  3. 每个用户的"版本对象总大小"与 user_quotas.used_bytes 是否一致。
//
// 默认只报告（dry-run）。加 -fix 会把缺失记录的孤儿对象登记为 deleting，
// 交给 storage 服务的后台 GC 清理；配额差异只报告，不自动改账。
//
// 用法：
//
//	go run ./tools/reconcile -config service/storage/rpc/etc/storage.yaml
//	go run ./tools/reconcile -config deploy/config/storage.rpc.yaml -fix
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"time"

	"cloud-drive/common/objectstore"

	_ "github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/conf"
)

type config struct {
	Mysql struct {
		DataSource string
	}
	Minio objectstore.Config
}

func main() {
	configFile := flag.String("f", "service/storage/rpc/etc/storage.yaml", "存储服务配置文件（用于读取 DSN 与 MinIO 配置）")
	dsn := flag.String("dsn", "", "覆盖 MySQL DSN（file/storage 用户库在同一个实例时可直接复用）")
	fix := flag.Bool("fix", false, "把无记录的孤儿对象标记为待删除")
	timeout := flag.Duration("timeout", 5*time.Minute, "整体超时")
	flag.Parse()

	var c config
	conf.MustLoad(*configFile, &c)

	conn := *dsn
	if conn == "" {
		conn = c.Mysql.DataSource
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	db, err := sql.Open("mysql", conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "连接数据库失败：%v\n", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "数据库不可用：%v\n", err)
		os.Exit(1)
	}

	store, err := objectstore.New(c.Minio)
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化对象存储失败：%v\n", err)
		os.Exit(1)
	}

	reporter := &report{db: db, store: store, fix: *fix}

	fmt.Println("== 1. 数据库中标记可用的对象是否存在 ==")
	reporter.checkDatabaseObjects(ctx)

	fmt.Println("\n== 2. MinIO 中的孤儿对象 ==")
	reporter.checkOrphanObjects(ctx)

	fmt.Println("\n== 3. 配额账目差异 ==")
	reporter.checkQuota(ctx)

	fmt.Printf("\n完成：对象缺失 %d 个，孤儿对象 %d 个，配额不一致用户 %d 个\n",
		reporter.missingObjects, reporter.orphanObjects, reporter.quotaMismatch)
	if reporter.missingObjects > 0 || reporter.quotaMismatch > 0 {
		os.Exit(2)
	}
}

type report struct {
	db    *sql.DB
	store *objectstore.Store
	fix   bool

	missingObjects int
	orphanObjects  int
	quotaMismatch  int
}

func (r *report) checkDatabaseObjects(ctx context.Context) {
	rows, err := r.db.QueryContext(ctx,
		"select `id`, `object_key`, `size` from `storage_objects` where `status` = 'available'")
	if err != nil {
		fmt.Printf("  查询失败：%v\n", err)
		return
	}
	defer rows.Close()

	checked := 0
	for rows.Next() {
		var (
			id   uint64
			key  string
			size uint64
		)
		if err := rows.Scan(&id, &key, &size); err != nil {
			fmt.Printf("  扫描失败：%v\n", err)
			continue
		}
		checked++

		info, err := r.store.Stat(ctx, key)
		if err != nil {
			r.missingObjects++
			fmt.Printf("  ✗ 记录 id=%d key=%s 在对象存储中不存在（err=%v）\n", id, key, err)
			continue
		}
		if uint64(info.Size) != size {
			fmt.Printf("  ! 记录 id=%d key=%s 大小不一致：db=%d object=%d\n", id, key, size, info.Size)
		}
	}
	fmt.Printf("  已检查 %d 条记录\n", checked)
}

func (r *report) checkOrphanObjects(ctx context.Context) {
	objects, err := r.store.List(ctx, "u/")
	if err != nil {
		fmt.Printf("  列举对象失败：%v\n", err)
		return
	}

	known := make(map[string]struct{}, len(objects))
	rows, err := r.db.QueryContext(ctx, "select `object_key` from `storage_objects`")
	if err != nil {
		fmt.Printf("  查询失败：%v\n", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			continue
		}
		known[key] = struct{}{}
	}

	orphans := 0
	for _, obj := range objects {
		if _, ok := known[obj.Key]; ok {
			continue
		}
		orphans++
		r.orphanObjects++
		fmt.Printf("  ✗ 孤儿对象 key=%s size=%d modified=%s\n",
			obj.Key, obj.Size, obj.LastModified.Format(time.RFC3339))

		if !r.fix {
			continue
		}
		// 登记为待删除，由 storage 的后台 GC 真正删除。
		if _, err := r.db.ExecContext(ctx,
			"insert into `storage_objects` (`object_key`,`backend`,`bucket_name`,`size`,"+
				"`content_type`,`status`,`last_error`) values (?, 'minio', ?, ?, "+
				"'application/octet-stream', 'deleting', 'orphan object found by reconcile')",
			obj.Key, r.store.Bucket(), obj.Size); err != nil {
			fmt.Printf("    登记失败：%v\n", err)
		} else {
			fmt.Println("    → 已登记为 deleting，等待后台 GC")
		}
	}
	if orphans == 0 {
		fmt.Println("  没有孤儿对象")
	}
}

func (r *report) checkQuota(ctx context.Context) {
	// 先按 (owner_id, object_key) 去重，避免回滚产生的多个版本引用同一对象时重复计算。
	rows, err := r.db.QueryContext(ctx, `
		select t.owner_id, t.total, q.used_bytes
		  from (select owner_id, sum(size) as total
		          from (select distinct owner_id, object_key, size from file_versions) d
		         group by owner_id) t
		  left join user_quotas q on q.user_id = t.owner_id`)
	if err != nil {
		fmt.Printf("  查询失败（file_versions 与 user_quotas 是否在同一个库？）：%v\n", err)
		return
	}
	defer rows.Close()

	checked := 0
	for rows.Next() {
		var (
			ownerId uint64
			total   uint64
			used    sql.NullInt64
		)
		if err := rows.Scan(&ownerId, &total, &used); err != nil {
			fmt.Printf("  扫描失败：%v\n", err)
			continue
		}
		checked++
		if !used.Valid {
			fmt.Printf("  ✗ 用户 %d 有版本数据但没有配额记录\n", ownerId)
			r.quotaMismatch++
			continue
		}
		// used_bytes 包含只存在于当前版本（尚未写入 file_versions）的对象，因此只报告明显偏小的情况。
		if uint64(used.Int64) < total {
			fmt.Printf("  ✗ 用户 %d 配额偏小：used_bytes=%d 版本总大小=%d（差 %d）\n",
				ownerId, used.Int64, total, int64(total)-used.Int64)
			r.quotaMismatch++
		}
	}
	if checked == 0 {
		fmt.Println("  没有可对账的用户")
	}
}
