import { describe, it, expect, vi, beforeEach } from 'vitest'
import { generationApi, uploadApi } from './generation'
import { client, LONG_RUNNING_TIMEOUT } from './client'
import { useAuthStore } from '@/stores/auth'
import type { ApiResponse } from '@/types/common'
import type { GenerationTask, TaskDeleteAllResponse } from '@/types/generation'

// 回归测试覆盖的 bug：topbar 的剩余额度取自 authStore 缓存的 user 快照，
// 只在登录/initialize 时写入；生成一次任务（消耗额度）或删除一条任务
// （可能退还额度）之后，服务端的 quota_used 已经变了，但本地快照不会
// 自动跟着变，topbar 数字要等到下次整页刷新才会更新。修复方式是
// generationApi 里改配额的四个动作（生成图片/视频、删单条、清空）成功后
// 顺带调一次 authStore.refreshUser()——这里直接 spy client 上真正发请求
// 的方法（post/get/delete），走真实的 generationApi 实现（而不是像页面
// 测试那样整体 vi.mock 掉 @/api/generation，那样根本测不到这段接线），
// 断言 refreshUser 确实在动作成功后被调用，且改配额之外的接口（getTask/
// listTasks）不会被误连带触发。
vi.mock('@/stores/auth', () => ({
  useAuthStore: { getState: vi.fn() },
}))

const task: GenerationTask = {
  id: 1,
  mode: 'imggen',
  model: 'qwen-image',
  status: 'PENDING',
  prompt: 'a cat',
  params: {},
  inputUrls: [],
  resultUrls: [],
  createdAt: '2026-07-19T00:00:00Z',
  updatedAt: '2026-07-19T00:00:00Z',
}

const taskResponse: ApiResponse<GenerationTask> = { code: 'OK', data: task }
const deleteAllResponse: ApiResponse<TaskDeleteAllResponse> = { code: 'OK', data: { deleted: 3 } }
const nullResponse: ApiResponse<null> = { code: 'OK', data: null }

describe('generationApi 配额相关动作，成功后刷新 authStore 缓存的 user', () => {
  const refreshUser = vi.fn().mockResolvedValue(undefined)

  beforeEach(() => {
    refreshUser.mockClear()
    vi.mocked(useAuthStore.getState).mockReturnValue({ refreshUser } as unknown as ReturnType<
      typeof useAuthStore.getState
    >)
  })

  it('generateImage 成功后刷新 user', async () => {
    vi.spyOn(client, 'post').mockResolvedValue({ data: taskResponse })

    const result = await generationApi.generateImage({ model: 'qwen-image', prompt: 'a cat' })

    expect(result).toEqual(task)
    expect(refreshUser).toHaveBeenCalledTimes(1)
  })

  it('generateVideo 成功后刷新 user', async () => {
    vi.spyOn(client, 'post').mockResolvedValue({ data: taskResponse })

    await generationApi.generateVideo({ model: 'wan-t2v', prompt: 'a dog' })

    expect(refreshUser).toHaveBeenCalledTimes(1)
  })

  it('deleteTask 成功后刷新 user', async () => {
    vi.spyOn(client, 'delete').mockResolvedValue({ data: nullResponse })

    await generationApi.deleteTask(1)

    expect(refreshUser).toHaveBeenCalledTimes(1)
  })

  it('deleteAllTasks 成功后刷新 user', async () => {
    vi.spyOn(client, 'delete').mockResolvedValue({ data: deleteAllResponse })

    const result = await generationApi.deleteAllTasks()

    expect(result).toEqual({ deleted: 3 })
    expect(refreshUser).toHaveBeenCalledTimes(1)
  })

  it('生成失败时不刷新 user（配额动作没有真的成功）', async () => {
    vi.spyOn(client, 'post').mockRejectedValue(new Error('boom'))

    await expect(
      generationApi.generateImage({ model: 'qwen-image', prompt: 'a cat' }),
    ).rejects.toThrow('boom')
    expect(refreshUser).not.toHaveBeenCalled()
  })

  it('getTask/listTasks 不改配额，不触发刷新', async () => {
    vi.spyOn(client, 'get').mockResolvedValue({ data: taskResponse })

    await generationApi.getTask(1)

    expect(refreshUser).not.toHaveBeenCalled()
  })
})

/**
 * 回归测试覆盖的 bug：耗时长的生成类接口沿用了 client 上 15s 的全局默认
 * 超时，而它们的真实耗时远超这个值——实测 /api/optimize-prompt 18.56s、
 * /api/generate/image 39.68s（后端对 t8star 的上游超时本就是 180s）。
 * 结果是浏览器每次都在 15s 处中止请求、报「网络连接失败」，而服务端把这次
 * 生成完整跑完了：图真的出了、token 真的扣了、配额也真的扣了，用户只是
 * 永远看不到结果。
 *
 * 这里断言的是「这几个接口带了显式的长超时」这条接线本身——它一旦被误删，
 * 症状会退回到那个既失败又计费的状态，而单元测试里没有真实耗时能自然暴露
 * 它，只能显式钉住。
 */
describe('长耗时接口使用显式的长超时，而不是 15s 全局默认值', () => {
  beforeEach(() => {
    // spyOn 装上的替身会跨用例存活，不还原的话 post.mock.calls[0] 拿到的
    // 可能是上一个用例留下的调用记录，断言就测不到本用例真正发出的请求。
    vi.restoreAllMocks()
    vi.mocked(useAuthStore.getState).mockReturnValue({
      refreshUser: vi.fn().mockResolvedValue(undefined),
    } as unknown as ReturnType<typeof useAuthStore.getState>)
  })

  it('generateImage 带 LONG_RUNNING_TIMEOUT', async () => {
    const post = vi.spyOn(client, 'post').mockResolvedValue({ data: taskResponse })

    await generationApi.generateImage({ model: 'gpt-image-2', prompt: 'a cat' })

    expect(post.mock.calls[0][2]).toMatchObject({ timeout: LONG_RUNNING_TIMEOUT })
  })

  it('optimizePrompt 带 LONG_RUNNING_TIMEOUT', async () => {
    const post = vi
      .spyOn(client, 'post')
      .mockResolvedValue({ data: { code: 'OK', data: { prompt: 'x', model: 'y' } } })

    await generationApi.optimizePrompt({ draft: 'a cat', mode: 't2i' })

    expect(post.mock.calls[0][2]).toMatchObject({ timeout: LONG_RUNNING_TIMEOUT })
  })

  // 上传的耗时取决于文件大小和上行带宽，同样可能轻易超过 15s——是同一类
  // 「静默失败但服务端已完成」的坑。
  it('upload 带 LONG_RUNNING_TIMEOUT', async () => {
    const post = vi
      .spyOn(client, 'post')
      .mockResolvedValue({ data: { code: 'OK', data: { url: 'https://x/y.png' } } })

    await uploadApi.upload(new File(['x'], 'y.png', { type: 'image/png' }))

    expect(post.mock.calls[0][2]).toMatchObject({ timeout: LONG_RUNNING_TIMEOUT })
  })

  // 反面：创建视频任务是异步接口，提交后立刻返回 task id，真正的等待在
  // 后续轮询里。给它加长超时没有意义，也会掩盖「提交这一步卡住了」。
  it('generateVideo 不加长超时（它是立即返回 task id 的异步接口）', async () => {
    const post = vi.spyOn(client, 'post').mockResolvedValue({ data: taskResponse })

    await generationApi.generateVideo({ model: 'wan2.7-t2v', prompt: 'a cat' })

    expect(post.mock.calls[0][2]).toBeUndefined()
  })
})
