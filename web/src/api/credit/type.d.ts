/** 当前用户的积分信息 */
export interface CreditsDto {
  /** 积分总余额 */
  balance: number;
  /** 被进行中任务冻结的积分 */
  frozen: number;
  /** 可用积分 */
  available: number;
}
