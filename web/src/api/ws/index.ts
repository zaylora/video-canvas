import service from '@/utils/requests/service'
import type { WsTicketDto } from './type'

/**
 * 获取 WebSocket 一次性 ticket，30s 内有效；不要把 JWT 放进 WebSocket URL
 * @returns WebSocket ticket
 */
export const getWsTicket = () => service.post<WsTicketDto>('/ws/ticket', undefined)
