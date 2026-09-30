/** WebSocket 一次性连接凭证 */
export interface WsTicketDto {
  /** 用于建立连接的一次性 ticket */
  ticket: string;
  /** 有效期（秒） */
  expires_in: number;
}
