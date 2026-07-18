package apperr_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
)

func TestSentinels_HaveCodeAndStatus(t *testing.T) {
	assert.Equal(t, "AUTH_INVALID_CREDENTIALS", apperr.ErrInvalidCredentials.Code)
	assert.Equal(t, http.StatusUnauthorized, apperr.ErrInvalidCredentials.HTTPStatus)
	assert.Equal(t, "USER_LAST_ADMIN", apperr.ErrLastAdmin.Code)
	assert.Equal(t, http.StatusUnprocessableEntity, apperr.ErrLastAdmin.HTTPStatus)
}

// Wrap 必须返回副本，否则并发下会互相覆盖 Internal 字段。
func TestWrap_DoesNotMutateSentinel(t *testing.T) {
	cause := errors.New("boom")
	wrapped := apperr.ErrInternal.Wrap(cause)

	assert.Nil(t, apperr.ErrInternal.Internal, "哨兵不得被修改")
	assert.Equal(t, cause, wrapped.Internal)
	assert.Equal(t, apperr.ErrInternal.Code, wrapped.Code)
	assert.NotSame(t, apperr.ErrInternal, wrapped)
}

func TestAs_ExtractsAppError(t *testing.T) {
	err := error(apperr.ErrUserNotFound.Wrap(errors.New("no rows")))

	var target *apperr.AppError
	require.True(t, errors.As(err, &target))
	assert.Equal(t, "USER_NOT_FOUND", target.Code)
}

func TestUnwrap_ReachesCause(t *testing.T) {
	cause := errors.New("root cause")
	err := apperr.ErrInternal.Wrap(cause)
	assert.True(t, errors.Is(err, cause))
}
