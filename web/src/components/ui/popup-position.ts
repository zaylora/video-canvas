import type { Popover as PopoverPrimitive } from "@base-ui/react/popover"

/**
 * 弹出框（弹层、下拉、菜单）的碰撞策略：方向固定、不翻面。
 * base-ui 默认空间不够时会翻到触发器的另一侧，弹出框的位置就随窗口、输入框高度跳来跳去；
 * 这里固定在调用方指定的一侧（正上方 / 正下方），只在水平方向上平移，免得超出屏幕。
 */
export const FIXED_SIDE_COLLISION: NonNullable<
  PopoverPrimitive.Positioner.Props["collisionAvoidance"]
> = { side: "none", align: "shift", fallbackAxisSide: "none" }
