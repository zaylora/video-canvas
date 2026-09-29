import service from '@/utils/requests/service'
import type { CreditsDto } from './type'

/**
 * 获取当前用户积分
 * @returns 积分信息
 */
export const getCredits = () => service.get<CreditsDto>('/credits', undefined)
