package logic

import (
	"context"
	"database/sql"

	"cloud-drive/service/user/model"
	"cloud-drive/service/user/rpc/internal/config"
	"cloud-drive/service/user/rpc/internal/svc"
)

const (
	testSalt      = "test-salt"
	testJWTSecret = "test-jwt-secret"
)

func newTestSvc(users model.UsersModel, quotas model.UserQuotasModel, tokens model.RefreshTokensModel) *svc.ServiceContext {
	return &svc.ServiceContext{
		Config:      config.Config{Salt: testSalt, JwtSecret: testJWTSecret},
		UsersModel:  users,
		QuotasModel: quotas,
		TokensModel: tokens,
	}
}

// fakeResult 用来模拟 Insert 返回的 sql.Result。
type fakeResult struct {
	id   int64
	rows int64
}

func (r fakeResult) LastInsertId() (int64, error) { return r.id, nil }
func (r fakeResult) RowsAffected() (int64, error) { return r.rows, nil }

// ---------------- users ----------------

type fakeUsersModel struct {
	findOne           func(ctx context.Context, id uint64) (*model.Users, error)
	findOneByUsername func(ctx context.Context, username string) (*model.Users, error)
	findOneByEmail    func(ctx context.Context, email sql.NullString) (*model.Users, error)
	insert            func(ctx context.Context, data *model.Users) (sql.Result, error)
	update            func(ctx context.Context, data *model.Users) error
	del               func(ctx context.Context, id uint64) error
}

var _ model.UsersModel = (*fakeUsersModel)(nil)

func newFakeUsersModel() *fakeUsersModel {
	return &fakeUsersModel{
		findOne: func(context.Context, uint64) (*model.Users, error) {
			return nil, model.ErrNotFound
		},
		findOneByUsername: func(context.Context, string) (*model.Users, error) {
			return nil, model.ErrNotFound
		},
		findOneByEmail: func(context.Context, sql.NullString) (*model.Users, error) {
			return nil, model.ErrNotFound
		},
		insert: func(context.Context, *model.Users) (sql.Result, error) {
			return fakeResult{id: 1, rows: 1}, nil
		},
		update: func(context.Context, *model.Users) error { return nil },
		del:    func(context.Context, uint64) error { return nil },
	}
}

func (f *fakeUsersModel) FindOne(ctx context.Context, id uint64) (*model.Users, error) {
	return f.findOne(ctx, id)
}

func (f *fakeUsersModel) FindOneByUsername(ctx context.Context, username string) (*model.Users, error) {
	return f.findOneByUsername(ctx, username)
}

func (f *fakeUsersModel) FindOneByEmail(ctx context.Context, email sql.NullString) (*model.Users, error) {
	return f.findOneByEmail(ctx, email)
}

func (f *fakeUsersModel) Insert(ctx context.Context, data *model.Users) (sql.Result, error) {
	return f.insert(ctx, data)
}

func (f *fakeUsersModel) Update(ctx context.Context, data *model.Users) error {
	return f.update(ctx, data)
}

func (f *fakeUsersModel) Delete(ctx context.Context, id uint64) error {
	return f.del(ctx, id)
}

// ---------------- user_quotas ----------------

type fakeQuotasModel struct {
	insert       func(ctx context.Context, data *model.UserQuotas) (sql.Result, error)
	findOne      func(ctx context.Context, id uint64) (*model.UserQuotas, error)
	findByUserId func(ctx context.Context, userId uint64) (*model.UserQuotas, error)
	update       func(ctx context.Context, data *model.UserQuotas) error
	del          func(ctx context.Context, id uint64) error
	reserveQuota func(ctx context.Context, userId, bytes uint64) (bool, error)
	releaseQuota func(ctx context.Context, userId, bytes uint64) (bool, error)
}

var _ model.UserQuotasModel = (*fakeQuotasModel)(nil)

func newFakeQuotasModel() *fakeQuotasModel {
	return &fakeQuotasModel{
		insert: func(context.Context, *model.UserQuotas) (sql.Result, error) {
			return fakeResult{id: 1, rows: 1}, nil
		},
		findOne: func(context.Context, uint64) (*model.UserQuotas, error) {
			return nil, model.ErrNotFound
		},
		findByUserId: func(context.Context, uint64) (*model.UserQuotas, error) {
			return nil, model.ErrNotFound
		},
		update:       func(context.Context, *model.UserQuotas) error { return nil },
		del:          func(context.Context, uint64) error { return nil },
		reserveQuota: func(context.Context, uint64, uint64) (bool, error) { return false, nil },
		releaseQuota: func(context.Context, uint64, uint64) (bool, error) { return false, nil },
	}
}

func (f *fakeQuotasModel) Insert(ctx context.Context, data *model.UserQuotas) (sql.Result, error) {
	return f.insert(ctx, data)
}

func (f *fakeQuotasModel) FindOne(ctx context.Context, id uint64) (*model.UserQuotas, error) {
	return f.findOne(ctx, id)
}

func (f *fakeQuotasModel) FindOneByUserId(ctx context.Context, userId uint64) (*model.UserQuotas, error) {
	return f.findByUserId(ctx, userId)
}

func (f *fakeQuotasModel) Update(ctx context.Context, data *model.UserQuotas) error {
	return f.update(ctx, data)
}

func (f *fakeQuotasModel) Delete(ctx context.Context, id uint64) error {
	return f.del(ctx, id)
}

func (f *fakeQuotasModel) ReserveQuota(ctx context.Context, userId, bytes uint64) (bool, error) {
	return f.reserveQuota(ctx, userId, bytes)
}

func (f *fakeQuotasModel) ReleaseQuota(ctx context.Context, userId, bytes uint64) (bool, error) {
	return f.releaseQuota(ctx, userId, bytes)
}

// ---------------- refresh_tokens ----------------

type fakeTokensModel struct {
	insert          func(ctx context.Context, data *model.RefreshTokens) (sql.Result, error)
	findOne         func(ctx context.Context, id uint64) (*model.RefreshTokens, error)
	findByTokenHash func(ctx context.Context, tokenHash string) (*model.RefreshTokens, error)
	update          func(ctx context.Context, data *model.RefreshTokens) error
	del             func(ctx context.Context, id uint64) error
}

var _ model.RefreshTokensModel = (*fakeTokensModel)(nil)

func newFakeTokensModel() *fakeTokensModel {
	return &fakeTokensModel{
		insert: func(context.Context, *model.RefreshTokens) (sql.Result, error) {
			return fakeResult{id: 1, rows: 1}, nil
		},
		findOne: func(context.Context, uint64) (*model.RefreshTokens, error) {
			return nil, model.ErrNotFound
		},
		findByTokenHash: func(context.Context, string) (*model.RefreshTokens, error) {
			return nil, model.ErrNotFound
		},
		update: func(context.Context, *model.RefreshTokens) error { return nil },
		del:    func(context.Context, uint64) error { return nil },
	}
}

func (f *fakeTokensModel) Insert(ctx context.Context, data *model.RefreshTokens) (sql.Result, error) {
	return f.insert(ctx, data)
}

func (f *fakeTokensModel) FindOne(ctx context.Context, id uint64) (*model.RefreshTokens, error) {
	return f.findOne(ctx, id)
}

func (f *fakeTokensModel) FindOneByTokenHash(ctx context.Context, tokenHash string) (*model.RefreshTokens, error) {
	return f.findByTokenHash(ctx, tokenHash)
}

func (f *fakeTokensModel) Update(ctx context.Context, data *model.RefreshTokens) error {
	return f.update(ctx, data)
}

func (f *fakeTokensModel) Delete(ctx context.Context, id uint64) error {
	return f.del(ctx, id)
}
