package ossx

// 白盒测试（package ossx 而非 ossx_test）：PutPublic 的关键性质是「把调用方
// 给的 io.Reader 原样透传给 SDK，中途不缓冲」，而 SDK 那一层只有真正联网才
// 会走到。为了在不联网的前提下断言这条性质，Client 上留了一个未导出的
// putter 注入点（见 client.go），只有同包测试能设置它——这与
// internal/service/upload_oss_test.go 用白盒测试 settingsOSSResolver 的
// newSTSClient 注入点是同一种做法。
//
// 必须说清这条测试的边界：它验证的是「我们这一层没有把 body 读干净」，
// 不验证阿里云 SDK 内部是否流式（那需要真实网络）。SDK 侧的依据是
// bucket.PutObject(key, reader, ...) -> DoPutObject -> conn.do 直接把 reader
// 当作 http.Request.Body，只有在显式开启 MD5（IsEnableMD5，默认关闭）时才会
// 走 calcMD5 的整体缓冲分支。

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePutter 记录 PutPublic 交给 SDK 的 key / reader / options，不做任何网络
// 操作。readAll 控制它是否消费 reader——只有需要观察读取行为的用例才打开。
type fakePutter struct {
	calls   int
	key     string
	reader  io.Reader
	options []oss.Option
	readAll bool
	body    []byte
	err     error
}

func (f *fakePutter) PutObject(key string, reader io.Reader, options ...oss.Option) error {
	f.calls++
	f.key = key
	f.reader = reader
	f.options = options
	if f.readAll {
		b, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		f.body = b
	}
	return f.err
}

// oneByteReader 每次 Read 只吐 1 个字节并计数。任何试图「一口气 io.ReadAll
// 掉」的实现都会把 reads 打到 len(data)+1，而流式透传的实现在离开我们这层时
// reads 仍然是 0。
type oneByteReader struct {
	data  []byte
	pos   int
	reads int
}

func (r *oneByteReader) Read(p []byte) (int, error) {
	r.reads++
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = r.data[r.pos]
	r.pos++
	return 1, nil
}

func newTestClientWithPutter(cfg Config, p *fakePutter) *Client {
	c := NewDirectClient(cfg, "fake-ak", "fake-sk")
	c.putter = func(context.Context) (objectPutter, error) { return p, nil }
	return c
}

func TestClient_PutPublic_ReturnsPermanentPublicURL(t *testing.T) {
	p := &fakePutter{}
	c := newTestClientWithPutter(Config{Bucket: "trans-ai-cn", Region: "oss-cn-chengdu"}, p)

	url, err := c.PutPublic(context.Background(), "results/42/0-a1b2c3d4.png", strings.NewReader("body"), "image/png")
	require.NoError(t, err)

	// 形状写死：这是要存进数据库、要被用户复制出去分享的永久地址，
	// 任何静默变形（换 CNAME、换内网 endpoint）都必须先让这条测试红。
	assert.Equal(t, "https://trans-ai-cn.oss-cn-chengdu.aliyuncs.com/results/42/0-a1b2c3d4.png", url)
	assert.Equal(t, "results/42/0-a1b2c3d4.png", p.key)
}

func TestClient_PutPublic_SetsPublicReadACLAndHeaders(t *testing.T) {
	p := &fakePutter{}
	c := newTestClientWithPutter(Config{Bucket: "b", Region: "oss-cn-chengdu"}, p)

	_, err := c.PutPublic(context.Background(), "results/1/0-deadbeef.mp4", strings.NewReader("body"), "video/mp4")
	require.NoError(t, err)

	acl, err := oss.FindOption(p.options, oss.HTTPHeaderOssObjectACL, nil)
	require.NoError(t, err)
	// 没有 public-read，返回的「永久公开 URL」就是一个 403，
	// 归档看起来成功、实际全线坏掉。
	assert.Equal(t, string(oss.ACLPublicRead), acl)

	ct, err := oss.FindOption(p.options, oss.HTTPHeaderContentType, nil)
	require.NoError(t, err)
	assert.Equal(t, "video/mp4", ct)

	cc, err := oss.FindOption(p.options, oss.HTTPHeaderCacheControl, nil)
	require.NoError(t, err)
	assert.Equal(t, cacheControl, cc)
}

// TestClient_PutPublic_DoesNotBufferBody 是「视频不能整体进内存」这条要求的
// 守卫：断言 body 离开 PutPublic 时一个字节都没被读过，且交给 SDK 的就是原
// reader 本身（不是某个 bytes.Reader 副本）。
func TestClient_PutPublic_DoesNotBufferBody(t *testing.T) {
	body := &oneByteReader{data: []byte("pretend this is a 40MB video")}
	p := &fakePutter{}
	c := newTestClientWithPutter(Config{Bucket: "b", Region: "oss-cn-chengdu"}, p)

	_, err := c.PutPublic(context.Background(), "k.mp4", body, "video/mp4")
	require.NoError(t, err)

	assert.Equal(t, 0, body.reads, "PutPublic 不得在交给 SDK 之前读取 body")
	assert.Same(t, body, p.reader, "必须把原 reader 透传给 SDK，而不是先拷进内存再包一层")
}

// TestClient_PutPublic_BodyIsConsumableIncrementally 补上另一半：透传下去的
// reader 确实能被逐块消费（内容不丢、不乱序），所以「不缓冲」不是靠丢数据换来的。
func TestClient_PutPublic_BodyIsConsumableIncrementally(t *testing.T) {
	body := &oneByteReader{data: []byte("hello-oss")}
	p := &fakePutter{readAll: true}
	c := newTestClientWithPutter(Config{Bucket: "b", Region: "oss-cn-chengdu"}, p)

	_, err := c.PutPublic(context.Background(), "k.txt", body, "text/plain")
	require.NoError(t, err)

	assert.Equal(t, "hello-oss", string(p.body))
	assert.Greater(t, body.reads, 1, "内容应当是被分多次读走的（流式），而不是一次吃完")
}

func TestClient_PutPublic_UploadFailureReturnsError(t *testing.T) {
	p := &fakePutter{err: errors.New("oss 拒绝写入")}
	c := newTestClientWithPutter(Config{Bucket: "b", Region: "oss-cn-chengdu"}, p)

	url, err := c.PutPublic(context.Background(), "k.png", strings.NewReader("body"), "image/png")
	require.Error(t, err)
	// 失败时不能返回一个「看起来能用」的 URL，否则调用方会把坏地址写进库。
	assert.Empty(t, url)
}
