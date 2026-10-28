import { describe, expect, test } from "bun:test";

import {
  DRAMA_ERR,
  dramaErrorMessage,
  jobFailureMessage,
  isDramaError,
  isRevisionConflict,
} from "@/api/drama/errors";
import { ApiError } from "@/utils/requests/request";

const err = (code: number, status: number, msg = "后端文案") => new ApiError(msg, code, status);

describe("短剧错误码映射（契约 §8）", () => {
  test("67002 / 67006 后端 Msg 写了具体原因，优先展示 Msg", () => {
    expect(dramaErrorMessage(err(DRAMA_ERR.sourceInvalid, 400, "docx 里没有正文"))).toBe(
      "docx 里没有正文",
    );
    expect(dramaErrorMessage(err(DRAMA_ERR.stepNotReady, 400, "第 2 集还没有剧本"))).toBe(
      "第 2 集还没有剧本",
    );
  });

  test("Msg 为空时用本地兜底文案", () => {
    expect(dramaErrorMessage(err(DRAMA_ERR.sourceInvalid, 400, ""))).toContain("格式");
    expect(dramaErrorMessage(err(DRAMA_ERR.stepNotReady, 400, ""))).toContain("前置");
  });

  test("67003 超长、67004 忙、67005 锁定、67007 无模型各有固定文案", () => {
    expect(dramaErrorMessage(err(DRAMA_ERR.sourceTooLong, 400))).toContain("30 万字");
    expect(dramaErrorMessage(err(DRAMA_ERR.jobBusy, 409))).toContain("进行中");
    expect(dramaErrorMessage(err(DRAMA_ERR.episodeLocked, 409))).toContain("锁定");
    expect(dramaErrorMessage(err(DRAMA_ERR.noTextModel, 400))).toContain("文本模型");
    expect(dramaErrorMessage(err(DRAMA_ERR.outputInvalid, 502))).toContain("不扣积分");
  });

  test("不认识的错误用后端 Msg，再不行用兜底", () => {
    expect(dramaErrorMessage(err(12345, 500, "积分不足"))).toBe("积分不足");
    expect(dramaErrorMessage(new Error("x"), "保存失败")).toBe("保存失败");
  });

  test("isDramaError 按码判断", () => {
    expect(isDramaError(err(DRAMA_ERR.notFound, 404), DRAMA_ERR.notFound)).toBe(true);
    expect(isDramaError(new Error("x"), DRAMA_ERR.notFound)).toBe(false);
  });

  test("revision 冲突：409 但不是 67004 / 67005", () => {
    expect(isRevisionConflict(err(40900, 409))).toBe(true);
    expect(isRevisionConflict(err(DRAMA_ERR.jobBusy, 409))).toBe(false);
    expect(isRevisionConflict(err(DRAMA_ERR.episodeLocked, 409))).toBe(false);
    expect(isRevisionConflict(err(500, 500))).toBe(false);
  });
});

describe("任务失败文案 jobFailureMessage", () => {
  test("67008 用固定文案并写明不扣积分", () => {
    expect(jobFailureMessage({ errorCode: 67008, errorMessage: "解析失败" })).toContain("不扣积分");
  });

  test("其他错误用后端 Msg；Msg 为空用兜底", () => {
    expect(jobFailureMessage({ errorCode: 40001, errorMessage: "积分不足" })).toBe("积分不足");
    expect(jobFailureMessage({ errorCode: 0, errorMessage: "" })).toBe("任务失败");
  });
});

describe("二期错误码（契约 §12）", () => {
  test("67009 / 67011 / 67012 / 67013 有固定文案", () => {
    expect(dramaErrorMessage(err(DRAMA_ERR.segmentNotFound, 404, ""))).toContain("片段不存在");
    expect(dramaErrorMessage(err(DRAMA_ERR.takeInvalid, 400, ""))).toContain("成功");
    expect(dramaErrorMessage(err(DRAMA_ERR.segmentOrderInvalid, 400, ""))).toContain("全部片段");
    expect(dramaErrorMessage(err(DRAMA_ERR.storyboardHasTakes, 409, ""))).toContain("清空");
  });

  test("67013 是业务 409，不当作 revision 冲突", () => {
    expect(isRevisionConflict(err(DRAMA_ERR.storyboardHasTakes, 409))).toBe(false);
    expect(isRevisionConflict(err(40900, 409))).toBe(true);
  });
});
