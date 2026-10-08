import { Fragment, useEffect, useMemo } from "react";
import { useParams } from "react-router";

import { ConversationRecordView } from "@/components/conversation/conversation-record";
import { Composer } from "@/components/home/composer";
import { EmptyMark } from "@/components/home/empty-mark";
import { buildSampleConversations } from "@/constants/conversation-sample";

/**
 * 对话页（设计稿 docs/品牌包装/登录与首页改版原型，参照即梦对话页）：
 * 生成记录按日期分组，一条记录是「参考图 + 模式参数 + 提示词 + 结果 + 操作」，
 * 输入卡片固定在页面底部，和创作页共用同一份模式与草稿。
 * 对话接口还没有，记录是样例数据；打开时直接停在最新一条。
 */
export default function ConversationPage() {
  const { id } = useParams();
  const conversation = useMemo(
    () => buildSampleConversations().find((item) => item.id === id),
    [id],
  );

  /** 打开对话直接定位到最新一条，不做滚动动画 */
  useEffect(() => {
    window.scrollTo({ top: document.body.scrollHeight, behavior: "instant" });
  }, [id]);

  return (
    <div className="mx-auto flex min-h-[calc(100svh-3.5rem)] w-full max-w-260 flex-col px-4 md:px-6">
      <div className="flex-1 pb-2" aria-live="polite">
        {!conversation ? (
          <EmptyMark title="对话不存在" hint="它可能已被删除，从左侧选一段对话吧" />
        ) : conversation.records.length === 0 ? (
          <EmptyMark title="还没有记录" hint="在下面输入想法开始" />
        ) : (
          conversation.records.map((record, index) => {
            const newDay = index === 0 || conversation.records[index - 1].day !== record.day;
            return (
              <Fragment key={record.id}>
                {newDay && (
                  <h2 className="mt-1 mb-4.5 text-[22px] font-semibold tracking-tight">
                    {record.day}
                  </h2>
                )}
                <ConversationRecordView record={record} />
              </Fragment>
            );
          })
        )}
      </div>
      <div className="from-background via-background sticky bottom-0 z-10 bg-linear-to-t via-70% to-transparent pt-4 pb-5">
        <Composer placement="dock" />
      </div>
    </div>
  );
}
