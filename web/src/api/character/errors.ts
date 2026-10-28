/** 角色卡业务错误码（后端 errcode 66xxx 号段，契约 §4） */
export const CHARACTER_ERR = {
  /** 已有同名角色卡（名称唯一约束已取消，后端不再返回，常量保留） */
  nameDup: 66001,
  /** 角色卡数量已达上限 */
  limit: 66002,
  /** 引用的素材不可用（不存在 / 种类不对） */
  assetInvalid: 66003,
  /** 角色卡不存在 */
  notFound: 66004,
  /** 角色卡参数不合法 */
  invalid: 66005,
} as const;
