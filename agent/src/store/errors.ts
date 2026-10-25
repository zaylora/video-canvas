/** 记录不存在，或不属于这个用户（两者对调用方一样：不能泄露别人的数据是否存在） */
export class NotFoundError extends Error {
  constructor(what = "记录") {
    super(`${what}不存在`);
    this.name = "NotFoundError";
  }
}

/** 违反唯一约束：同一画布上已经有活跃的运行，或 ID 重复 */
export class DuplicateError extends Error {
  constructor(message = "记录已存在") {
    super(message);
    this.name = "DuplicateError";
  }
}

/**
 * 状态迁移的 CAS 没有命中：记录不存在，或当前状态已不在期望的集合里（被别的流程抢先处理了）。
 * 调用方应当把它当作「已被处理」，而不是失败。
 */
export class StateConflictError extends Error {
  constructor(message = "状态已经变化") {
    super(message);
    this.name = "StateConflictError";
  }
}
