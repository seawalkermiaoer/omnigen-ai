package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/handler"
	"github.com/chenhao/omnigen-ai/server/internal/middleware"
	"github.com/chenhao/omnigen-ai/server/internal/model/common"
	generationmodel "github.com/chenhao/omnigen-ai/server/internal/model/generation"
	usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/jwtx"
	"github.com/chenhao/omnigen-ai/server/internal/pkg/password"
	"github.com/chenhao/omnigen-ai/server/internal/repository"
)

// fakeDownloadTaskRepo is a minimal in-memory repository.TaskRepository —
// DownloadHandler only ever calls GetByIDForUser, so that's the only method
// with real ownership-filtering behavior; the rest just satisfy the
// interface.
type fakeDownloadTaskRepo struct {
	tasks map[int64]generationmodel.Task
}

func newFakeDownloadTaskRepo() *fakeDownloadTaskRepo {
	return &fakeDownloadTaskRepo{tasks: map[int64]generationmodel.Task{}}
}

func (f *fakeDownloadTaskRepo) put(t generationmodel.Task) { f.tasks[t.ID] = t }

func (f *fakeDownloadTaskRepo) Create(context.Context, *generationmodel.Task) error { return nil }

// GetByIDForUser mirrors the real repository's contract: a task that
// doesn't exist and a task that belongs to someone else are both
// apperr.ErrTaskNotFound — never a distinguishable error — so the fake has
// to enforce that itself rather than silently returning any row it finds.
func (f *fakeDownloadTaskRepo) GetByIDForUser(_ context.Context, id, userID int64) (*generationmodel.Task, error) {
	t, ok := f.tasks[id]
	if !ok || t.UserID != userID {
		return nil, apperr.ErrTaskNotFound
	}
	clone := t
	return &clone, nil
}

func (f *fakeDownloadTaskRepo) ListForUser(context.Context, int64, int, int) ([]generationmodel.Task, int64, error) {
	return nil, 0, nil
}
func (f *fakeDownloadTaskRepo) UpdateStatus(context.Context, int64, generationmodel.Status, string, string) error {
	return nil
}
func (f *fakeDownloadTaskRepo) UpdateResult(context.Context, int64, []string, map[string]any, string) error {
	return nil
}
func (f *fakeDownloadTaskRepo) ClaimPending(context.Context, int) ([]generationmodel.Task, error) {
	return nil, nil
}
func (f *fakeDownloadTaskRepo) DeleteForUser(context.Context, int64, int64) error { return nil }
func (f *fakeDownloadTaskRepo) DeleteAllForUser(context.Context, int64) (int64, error) {
	return 0, nil
}
func (f *fakeDownloadTaskRepo) RefundQuotaForTask(context.Context, int64) error { return nil }

var _ repository.TaskRepository = (*fakeDownloadTaskRepo)(nil)

// downloadTestUser is a logged-in user plus the token to authenticate as
// them.
type downloadTestUser struct {
	id    int64
	token string
}

// newDownloadTestEnv wires the real middleware chain (auth + error
// handling) around DownloadHandler, mirroring newUploadTestEnv /
// newImageGenTestEnv in the sibling test files. hostFilter is threaded
// straight into handler.NewDownloadHandlerWithHostFilter — the same
// constructor used in production, just with a filter tests can control so
// an httptest.Server's loopback host can stand in for a real allowlisted
// result host without the production allowlist itself ever growing a
// test-only entry.
func newDownloadTestEnv(t *testing.T, repo repository.TaskRepository, hostFilter func(string) bool) (*gin.Engine, downloadTestUser, downloadTestUser) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	userRepo := newMemRepo()
	jwtMgr := jwtx.NewManager("download-handler-test-secret", time.Hour)

	makeUser := func(username string) downloadTestUser {
		hash, err := password.Hash("password123")
		require.NoError(t, err)
		u := &usermodel.User{
			Username: username, PasswordHash: hash, DisplayName: username,
			Role: usermodel.RoleUser, Status: usermodel.StatusActive,
		}
		require.NoError(t, userRepo.Create(context.Background(), u))
		token, err := jwtMgr.Generate(u.ID, u.Role)
		require.NoError(t, err)
		return downloadTestUser{id: u.ID, token: token}
	}
	alice := makeUser("alice")
	bob := makeUser("bob")

	dlH := handler.NewDownloadHandlerWithHostFilter(repo, hostFilter)

	r := gin.New()
	r.Use(middleware.Recovery(), middleware.ErrorHandler())
	authed := r.Group("/api", middleware.Auth(jwtMgr, userRepo))
	authed.GET("/download/:taskId/:index", dlH.Download)

	return r, alice, bob
}

func downloadReq(path, token string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func decodeErrCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp.Code
}

// hostOnly strips the port from an httptest.Server URL, matching what
// url.URL.Hostname() / http.Request.URL.Hostname() hand the host filter.
func hostOnly(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	return u.Hostname()
}

func TestDownloadHandler_HappyPath_StreamsAndSetsContentDisposition(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake-png-bytes"))
	}))
	defer upstream.Close()

	repo := newFakeDownloadTaskRepo()
	allowedHost := hostOnly(t, upstream.URL)
	r, alice, _ := newDownloadTestEnv(t, repo, func(h string) bool { return h == allowedHost })

	repo.put(generationmodel.Task{
		ID: 1, UserID: alice.id, Mode: generationmodel.TaskModeImgGen,
		Status:     generationmodel.StatusSucceeded,
		ResultURLs: []string{upstream.URL + "/result.png"},
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, downloadReq("/api/download/1/0", alice.token))

	require.Equal(t, http.StatusOK, w.Code, "响应=%s", w.Body.String())
	assert.Equal(t, "fake-png-bytes", w.Body.String())
	assert.Equal(t, `attachment; filename="omnigen-imggen-1-0.png"`, w.Header().Get("Content-Disposition"))
	assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
}

func TestDownloadHandler_OtherUsersTask_404NotForbidden(t *testing.T) {
	repo := newFakeDownloadTaskRepo()
	r, alice, bob := newDownloadTestEnv(t, repo, func(string) bool { return true })

	repo.put(generationmodel.Task{
		ID: 1, UserID: bob.id, Mode: generationmodel.TaskModeImgGen,
		ResultURLs: []string{"https://cdn.aliyuncs.com/x.png"},
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, downloadReq("/api/download/1/0", alice.token))

	require.Equal(t, http.StatusNotFound, w.Code, "别人的任务必须是 404，不能是 403——否则 404/403 的区别本身就是存在性 oracle")
	assert.Equal(t, apperr.ErrTaskNotFound.Code(), decodeErrCode(t, w))
}

func TestDownloadHandler_NonexistentTask_404(t *testing.T) {
	repo := newFakeDownloadTaskRepo()
	r, alice, _ := newDownloadTestEnv(t, repo, func(string) bool { return true })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, downloadReq("/api/download/999/0", alice.token))

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, apperr.ErrTaskNotFound.Code(), decodeErrCode(t, w))
}

func TestDownloadHandler_IndexOutOfRange_422(t *testing.T) {
	repo := newFakeDownloadTaskRepo()
	r, alice, _ := newDownloadTestEnv(t, repo, func(string) bool { return true })

	repo.put(generationmodel.Task{
		ID: 1, UserID: alice.id, Mode: generationmodel.TaskModeImgGen,
		ResultURLs: []string{"https://cdn.aliyuncs.com/x.png"},
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, downloadReq("/api/download/1/5", alice.token))

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code, "响应=%s", w.Body.String())
	assert.Equal(t, apperr.ErrValidation.Code(), decodeErrCode(t, w))
}

func TestDownloadHandler_NegativeIndex_422(t *testing.T) {
	repo := newFakeDownloadTaskRepo()
	r, alice, _ := newDownloadTestEnv(t, repo, func(string) bool { return true })

	repo.put(generationmodel.Task{
		ID: 1, UserID: alice.id, Mode: generationmodel.TaskModeImgGen,
		ResultURLs: []string{"https://cdn.aliyuncs.com/x.png"},
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, downloadReq("/api/download/1/-1", alice.token))

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code, "响应=%s", w.Body.String())
	assert.Equal(t, apperr.ErrValidation.Code(), decodeErrCode(t, w))
}

func TestDownloadHandler_NonNumericTaskId_422(t *testing.T) {
	repo := newFakeDownloadTaskRepo()
	r, alice, _ := newDownloadTestEnv(t, repo, func(string) bool { return true })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, downloadReq("/api/download/not-a-number/0", alice.token))

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code, "响应=%s", w.Body.String())
	assert.Equal(t, apperr.ErrValidation.Code(), decodeErrCode(t, w))
}

func TestDownloadHandler_NonNumericIndex_422(t *testing.T) {
	repo := newFakeDownloadTaskRepo()
	r, alice, _ := newDownloadTestEnv(t, repo, func(string) bool { return true })

	repo.put(generationmodel.Task{
		ID: 1, UserID: alice.id, Mode: generationmodel.TaskModeImgGen,
		ResultURLs: []string{"https://cdn.aliyuncs.com/x.png"},
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, downloadReq("/api/download/1/not-a-number", alice.token))

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code, "响应=%s", w.Body.String())
	assert.Equal(t, apperr.ErrValidation.Code(), decodeErrCode(t, w))
}

func TestDownloadHandler_Unauthenticated_401(t *testing.T) {
	repo := newFakeDownloadTaskRepo()
	r, _, _ := newDownloadTestEnv(t, repo, func(string) bool { return true })

	w := httptest.NewRecorder()
	// 故意不带 Authorization header。
	r.ServeHTTP(w, downloadReq("/api/download/1/0", ""))

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// redirectChainServer serves GET /redirect?n=N: n>0 302s to n-1, n==0 (or
// missing) serves a 200 with a fixed body. Starting the chain at n=K
// produces exactly K redirect hops before the final 200 — a single
// predictable knob for both the "3 followed" and "4 rejected" tests below.
func redirectChainServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("n"))
		if n <= 0 {
			w.Header().Set("Content-Type", "video/mp4")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("final-hop-bytes"))
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/redirect?n=%d", n-1), http.StatusFound)
	}))
}

func TestDownloadHandler_RedirectChainOfThree_Followed(t *testing.T) {
	upstream := redirectChainServer()
	defer upstream.Close()

	repo := newFakeDownloadTaskRepo()
	allowedHost := hostOnly(t, upstream.URL)
	r, alice, _ := newDownloadTestEnv(t, repo, func(h string) bool { return h == allowedHost })

	repo.put(generationmodel.Task{
		ID: 1, UserID: alice.id, Mode: generationmodel.TaskModeT2V,
		ResultURLs: []string{upstream.URL + "/redirect?n=3"},
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, downloadReq("/api/download/1/0", alice.token))

	require.Equal(t, http.StatusOK, w.Code, "3 跳重定向应该被跟随，响应=%s", w.Body.String())
	assert.Equal(t, "final-hop-bytes", w.Body.String())
}

func TestDownloadHandler_RedirectChainOfFour_Rejected(t *testing.T) {
	upstream := redirectChainServer()
	defer upstream.Close()

	repo := newFakeDownloadTaskRepo()
	allowedHost := hostOnly(t, upstream.URL)
	r, alice, _ := newDownloadTestEnv(t, repo, func(h string) bool { return h == allowedHost })

	repo.put(generationmodel.Task{
		ID: 1, UserID: alice.id, Mode: generationmodel.TaskModeT2V,
		ResultURLs: []string{upstream.URL + "/redirect?n=4"},
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, downloadReq("/api/download/1/0", alice.token))

	assert.NotEqual(t, http.StatusOK, w.Code, "第 4 跳必须被拒绝")
	assert.Equal(t, apperr.ErrDownloadFailed.Code(), decodeErrCode(t, w))
}

func TestDownloadHandler_RedirectToNonAllowlistedHost_Rejected(t *testing.T) {
	// allowedEntry 是白名单内、唯一被信任的主机；它的响应把请求重定向到一个
	// 明确不在白名单里的主机（甚至不需要真的能连通——net/http 在跟随重定向
	// 前会先调用 CheckRedirect，被拒绝时根本不会对新主机发起 DNS/拨号，
	// 所以这里用一个不可达的域名就足以证明拦截发生在网络请求之前）。
	// 如果实现只检查了初始 URL 的主机（经典的绕过方式），这次重定向会被
	// 悄悄跟随而不是被拒绝。
	const disallowedRedirectTarget = "http://internal-service.invalid.example/evil"

	allowedEntry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, disallowedRedirectTarget, http.StatusFound)
	}))
	defer allowedEntry.Close()

	repo := newFakeDownloadTaskRepo()
	allowedHost := hostOnly(t, allowedEntry.URL) // "internal-service.invalid.example" 故意不在过滤器里
	r, alice, _ := newDownloadTestEnv(t, repo, func(h string) bool { return h == allowedHost })

	repo.put(generationmodel.Task{
		ID: 1, UserID: alice.id, Mode: generationmodel.TaskModeImgGen,
		ResultURLs: []string{allowedEntry.URL + "/start"},
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, downloadReq("/api/download/1/0", alice.token))

	assert.NotEqual(t, http.StatusOK, w.Code, "重定向落到白名单外的主机必须被拒绝，响应=%s", w.Body.String())
	assert.Equal(t, apperr.ErrDownloadFailed.Code(), decodeErrCode(t, w))
}

func TestDownloadHandler_HostAllowlist_ProductionSuffixes(t *testing.T) {
	cases := []struct {
		name string
		host string
		want bool
	}{
		{"dashscope 结果域名", "dashscope-result.oss-cn-beijing.aliyuncs.com", true},
		{"裸 aliyuncs.com", "aliyuncs.com", true},
		{"alicdn 子域名", "img.alicdn.com", true},
		{"t8star 结果域名（真实夹具里的 webstatic.aiproxy.vip，不是 API 域名 ai.t8star.org）", "webstatic.aiproxy.vip", true},
		{"云元数据端点（经典 SSRF 目标）", "169.254.169.254", false},
		{"仅仅是字符串包含 aliyuncs.com，前面没有 . 边界", "evil-aliyuncs.com", false},
		{"用真实域名做后缀伪装", "aliyuncs.com.evil.com", false},
		{"t8star 的 API 域名本身不在结果白名单里", "ai.t8star.org", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, handler.IsAllowedResultHost(tc.host), tc.host)
		})
	}
}

func TestDownloadHandler_HostAllowlist_MetadataEndpointRejected(t *testing.T) {
	repo := newFakeDownloadTaskRepo()
	// 生产过滤器：请求在联网之前就应该被拒绝，所以这里甚至不需要起一个
	// httptest.Server —— 如果实现在联网前没拦住，测试会因为真的去连
	// 169.254.169.254 而挂起/超时，本身就是失败信号。
	r, alice, _ := newDownloadTestEnv(t, repo, handler.IsAllowedResultHost)

	repo.put(generationmodel.Task{
		ID: 1, UserID: alice.id, Mode: generationmodel.TaskModeImgGen,
		ResultURLs: []string{"http://169.254.169.254/latest/meta-data/"},
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, downloadReq("/api/download/1/0", alice.token))

	assert.NotEqual(t, http.StatusOK, w.Code)
	assert.Equal(t, apperr.ErrDownloadFailed.Code(), decodeErrCode(t, w))
}

func TestDownloadHandler_HostAllowlist_EvilAliyuncsRejected(t *testing.T) {
	repo := newFakeDownloadTaskRepo()
	r, alice, _ := newDownloadTestEnv(t, repo, handler.IsAllowedResultHost)

	repo.put(generationmodel.Task{
		ID: 1, UserID: alice.id, Mode: generationmodel.TaskModeImgGen,
		ResultURLs: []string{"https://evil-aliyuncs.com/x"},
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, downloadReq("/api/download/1/0", alice.token))

	assert.NotEqual(t, http.StatusOK, w.Code)
	assert.Equal(t, apperr.ErrDownloadFailed.Code(), decodeErrCode(t, w))
}

func TestDownloadHandler_FilenameSanitization_PathTraversalNeutralized(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("bytes"))
	}))
	defer upstream.Close()

	repo := newFakeDownloadTaskRepo()
	allowedHost := hostOnly(t, upstream.URL)
	r, alice, _ := newDownloadTestEnv(t, repo, func(h string) bool { return h == allowedHost })

	// 最后一个路径段就是 "passwd" —— 没有扩展名，取名逻辑必须干净地退化
	// 到按 Content-Type 派生扩展名，而不是把 ".." 或路径分隔符带进
	// Content-Disposition。
	repo.put(generationmodel.Task{
		ID: 7, UserID: alice.id, Mode: generationmodel.TaskModeImgEdit,
		ResultURLs: []string{upstream.URL + "/x/../../../../etc/passwd"},
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, downloadReq("/api/download/7/0", alice.token))

	require.Equal(t, http.StatusOK, w.Code, "响应=%s", w.Body.String())
	cd := w.Header().Get("Content-Disposition")
	assert.NotContains(t, cd, "..")
	assert.NotContains(t, cd, "/")
	assert.NotContains(t, cd, "\\")
	assert.NotContains(t, cd, "etc")
	assert.NotContains(t, cd, "passwd")
	assert.Equal(t, `attachment; filename="omnigen-imgedit-7-0.png"`, cd)
}

func TestDownloadHandler_FilenameSanitization_CRLFInExtensionNeutralized(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("bytes"))
	}))
	defer upstream.Close()

	repo := newFakeDownloadTaskRepo()
	allowedHost := hostOnly(t, upstream.URL)
	r, alice, _ := newDownloadTestEnv(t, repo, func(h string) bool { return h == allowedHost })

	// %0d%0a 解码后是原始 CR LF，落在"扩展名"部分（最后一个 '.' 之后）——
	// 如果这段字节被原样拼进 Content-Disposition，就是一次响应头注入。
	repo.put(generationmodel.Task{
		ID: 8, UserID: alice.id, Mode: generationmodel.TaskModeImgGen,
		ResultURLs: []string{upstream.URL + "/evil.p%0d%0aX-Injected:1ng"},
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, downloadReq("/api/download/8/0", alice.token))

	require.Equal(t, http.StatusOK, w.Code, "响应=%s", w.Body.String())
	cd := w.Header().Get("Content-Disposition")
	assert.NotContains(t, cd, "\r")
	assert.NotContains(t, cd, "\n")
	assert.False(t, strings.Contains(cd, "X-Injected"), "CR/LF 之间的注入内容不应该出现在任何响应头里: %q", cd)
}

// TestDownloadHandler_HostAllowlist_ArchivedOSSBucketHostAllowed 是一颗钉子。
//
// 结果归档（service/result_archive.go）上线后，generation_tasks.result_urls
// 里存的不再是上游域名，而是我们自己 OSS bucket 的域名
// "<bucket>.<region>.aliyuncs.com"（见 ossx.Config.publicURL）。而「下载」
// 按钮走的仍然是 GET /api/download/:taskId/:index——它要过同一份 host 白名单。
// 白名单一旦不认这个域名，**所有新任务的下载会全部被拒**，而且是静默地全线坏掉。
//
// 今天它之所以通过，是因为白名单里有 ".aliyuncs.com" 这条（本来是给
// DashScope 结果域名用的），OSS bucket 域名恰好是它的子域，属于被动放行。
// 这条测试把「OSS 域名可下载」这个行为本身钉住：将来有人按设计文档
// （2026-07-19-result-archive-to-oss-design.md 第 149 行）把 ".aliyuncs.com"
// 收紧成「只放行当前配置的那个 bucket」时，如果忘了把 OSS 域名显式加回去，
// 这里会立刻红，而不是等用户报「下载全坏了」。
func TestDownloadHandler_HostAllowlist_ArchivedOSSBucketHostAllowed(t *testing.T) {
	ossHosts := []string{
		"omnigen-prod.oss-cn-chengdu.aliyuncs.com",
		"my-bucket.oss-cn-beijing.aliyuncs.com",
	}
	for _, host := range ossHosts {
		t.Run(host, func(t *testing.T) {
			assert.True(t, handler.IsAllowedResultHost(host),
				"归档后的结果 URL 就在这个 host 上，白名单不认它 = 所有新任务下载全挂")
		})
	}
}

// TestDownloadHandler_ArchivedOSSResultDownloadable 走完整 HTTP 路径，
// 而不只是断言那个谓词函数：任务的 result_urls 是一个 OSS 形状的 URL 时，
// /api/download 必须真的把字节流回来。上面那条钉的是白名单，这条钉的是
// 「整条下载链路对归档后的 URL 仍然可用」。
func TestDownloadHandler_ArchivedOSSResultDownloadable(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("ARCHIVED-BYTES"))
	}))
	defer upstream.Close()

	repo := newFakeDownloadTaskRepo()
	srvHost := hostOnly(t, upstream.URL)
	// 过滤器同时接受 httptest 的回环 host（字节真的要能取回来）和真实的
	// OSS bucket host——后者才是生产里 result_urls 的形状，用它来确认
	// 生产谓词也放行，两者缺一测试都说明不了问题。
	r, alice, _ := newDownloadTestEnv(t, repo, func(h string) bool {
		return h == srvHost || handler.IsAllowedResultHost(h)
	})
	require.True(t, handler.IsAllowedResultHost("omnigen-prod.oss-cn-chengdu.aliyuncs.com"))

	repo.put(generationmodel.Task{
		ID: 21, UserID: alice.id, Mode: generationmodel.TaskModeT2V,
		ResultURLs: []string{upstream.URL + "/results/21/0-a1b2c3d4.mp4"},
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, downloadReq("/api/download/21/0", alice.token))

	require.Equal(t, http.StatusOK, w.Code, "响应=%s", w.Body.String())
	assert.Equal(t, "ARCHIVED-BYTES", w.Body.String())
	assert.Contains(t, w.Header().Get("Content-Disposition"), ".mp4")
}
