import { describe, it, expect, vi, beforeEach } from 'vitest'
import { generationApi } from './generation'
import { client } from './client'
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
