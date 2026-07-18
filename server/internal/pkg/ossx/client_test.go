package ossx_test

import (
	"context"
	neturl "net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/pkg/ossx"
)

// TestClient_SignedURL_UsesFixedTTL exercises the *real* ossx.Client against
// fake (non-functional) credentials — this is safe because OSS's SignURL is
// a pure local HMAC computation over the request parameters (see
// aliyun-oss-go-sdk oss/bucket.go SignURL -> conn.signURL), it never makes a
// network call. That lets us assert the exact TTL baked into the signed URL
// without ever touching real OSS.
func TestClient_SignedURL_UsesFixedTTL(t *testing.T) {
	cfg := ossx.Config{Bucket: "test-bucket", Region: "oss-cn-hangzhou"}
	client := ossx.NewDirectClient(cfg, "fake-ak", "fake-sk")

	before := time.Now().Unix()
	url, err := client.SignedURL(context.Background(), "omnigen-uploads/123-abcdef0123456789.jpg")
	after := time.Now().Unix()
	require.NoError(t, err)

	parsed, err := neturl.Parse(url)
	require.NoError(t, err)

	expiresRaw := parsed.Query().Get("Expires")
	require.NotEmpty(t, expiresRaw, "签名 URL 必须带 Expires 查询参数")
	expires, err := strconv.ParseInt(expiresRaw, 10, 64)
	require.NoError(t, err)

	wantTTLSeconds := int64(ossx.SignedURLTTL / time.Second)
	require.Equal(t, int64(86400), wantTTLSeconds, "TTL 必须是 24 小时 = 86400 秒")

	// Expires = time.Now().Unix() + expiredInSec, computed inside the SDK at
	// call time (not at our `before`/`after` sample points), so allow a
	// couple of seconds of slack instead of asserting exact equality.
	assert.GreaterOrEqual(t, expires, before+wantTTLSeconds)
	assert.LessOrEqual(t, expires, after+wantTTLSeconds+2)
}

func TestConfig_EndpointDerivedFromRegion(t *testing.T) {
	// Region -> endpoint is exercised indirectly through SignedURL/Put; this
	// just pins the exact URL shape so a future refactor can't silently
	// change it (e.g. to an internal or CNAME endpoint) without a test
	// failing here first.
	cfg := ossx.Config{Bucket: "test-bucket", Region: "oss-cn-chengdu"}
	client := ossx.NewDirectClient(cfg, "ak", "sk")

	url, err := client.SignedURL(context.Background(), "k.jpg")
	require.NoError(t, err)
	assert.Contains(t, url, "oss-cn-chengdu.aliyuncs.com")
}
