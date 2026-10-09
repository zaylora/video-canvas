import { useCallback } from "react";
import { useLocation, useNavigate } from "react-router";

import type { GenerateKind, SubmitTarget } from "@/api/conversation/type";
import { useComposerStore } from "@/store/composer";
import { useConversationRecordsStore } from "@/store/conversation-records";
import type { SendState } from "@/utils/conversation/submission";

/** 一次发送要用的内容 */
export type SubmitArgs = {
  /** 创作模式，也是后端的生成种类 */
  kind: GenerateKind;
  /** 模型 key */
  modelId: string;
  /** 提示词原文 */
  prompt: string;
  /** evaluateSend 算出的结果：input、生成数量 */
  send: Pick<SendState, "input" | "count">;
};

/**
 * 输入卡片的发送：提交一条生成记录（见 store/conversation-records.ts 的 submit），
 * 成功后清空草稿并跳到记录所在的对话；请求失败时草稿原样保留，用户可以直接再发。
 * 发送期间输入卡片只读（composer.sending），避免重复提交。
 * @param target 提交到哪里：首页是默认创作，对话页是当前对话，新对话页是 new
 * @returns submit：发送；返回是否发送成功
 */
export function useConversationSubmit(target: SubmitTarget) {
  const navigate = useNavigate();
  const { pathname } = useLocation();

  const submit = useCallback(
    async (args: SubmitArgs): Promise<boolean> => {
      const composer = useComposerStore.getState();
      if (composer.sending) return false;
      composer.setSending(true);
      try {
        const result = await useConversationRecordsStore.getState().submit(target, {
          kind: args.kind,
          modelId: args.modelId,
          prompt: args.prompt,
          input: args.send.input,
          count: args.send.count,
        });
        useComposerStore.getState().clearDraft();
        const next = `/conversations/${result.conversationId}`;
        // 新对话页发完把地址换成真实对话，返回键不会回到空的「新对话」
        if (pathname !== next) navigate(next, { replace: target === "new" });
        return true;
      } catch {
        // 提示由请求层统一弹；草稿保留
        return false;
      } finally {
        useComposerStore.getState().setSending(false);
      }
    },
    [navigate, pathname, target],
  );

  return { submit };
}
