import { trimEngine } from "./trim-engine";
import { createTrimRunner } from "./trim-run";

/** 画布共用的修剪编码流程控制：编码任务不跟面板组件的挂载走，只有 abort 才会中止 */
export const trimRunner = createTrimRunner(trimEngine);
