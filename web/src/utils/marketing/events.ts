import { toast } from "sonner";

import { getMarketingProject, mapRound, mapScript } from "@/api/marketing";
import type { MarketingRoundEvent, MarketingScriptEvent, Round } from "@/api/marketing/type";
import { useMarketingStore } from "@/store/marketing";

import { roundTransition, scriptNotice } from "./merge";

/** 当前是不是正看着这个项目的详情页 */
const viewing = (projectId: string): boolean =>
  typeof window !== "undefined" && window.location.pathname === `/marketing/${projectId}`;

/** 路由跳转函数：由路由内的 WsRuntime 登记（这个模块在路由之外被 WebSocket 回调调用，拿不到 useNavigate） */
let navigator: ((path: string) => void) | null = null;

/**
 * 登记 / 注销路由跳转函数，提醒里的「查看」用它跳到项目详情页。
 * @param navigate 跳转函数；卸载时传 null
 */
export function setMarketingNavigator(navigate: ((path: string) => void) | null) {
  navigator = navigate;
}

/** 提醒里的「查看」：跳到项目详情页；还没登记跳转函数（不应出现）就整页跳转 */
function openProject(projectId: string) {
  const path = `/marketing/${projectId}`;
  if (navigator) navigator(path);
  else window.location.assign(path);
}

/** 本地没有这个项目（或缺这一轮）时重新拉一次详情；失败不打扰用户，下次重连会对账 */
export async function reloadMarketingProject(projectId: string): Promise<boolean> {
  try {
    useMarketingStore.getState().setProject(await getMarketingProject(projectId, true));
    return true;
  } catch {
    return false;
  }
}

/** 这次推送里事实卡环节是不是刚变成成功（新的一次运行，或同一次从未完成变成成功） */
export function factsJustSucceeded(prev: Round | undefined, next: Round): boolean {
  const after = next.steps.find((item) => item.step === "facts");
  if (after?.status !== "succeeded") return false;
  const before = prev?.steps.find((item) => item.step === "facts");
  return !before || before.id !== after.id || before.status !== "succeeded";
}

/**
 * 收到 marketing.round：并进项目；写脚本刚结束且用户不在这个项目的页面时，弹一条带「查看」的提醒。
 * 出片结果的提示由 marketing.script 负责，任务快照（task.updated）本身不弹通知。
 */
export function handleMarketingRoundEvent(event: MarketingRoundEvent) {
  const store = useMarketingStore.getState();
  const round = mapRound(event.round);
  const prev = store.projects[event.project_id]?.rounds.find((item) => item.id === round.id);
  if (!store.applyRound(event.project_id, round)) {
    if (store.watching[event.project_id]) void reloadMarketingProject(event.project_id);
  } else if (factsJustSucceeded(prev, round) && store.watching[event.project_id]) {
    // 事实卡写在项目上，轮次推送里没有：事实卡环节刚成功（含重新识别外观锚点）就拉一次详情
    void reloadMarketingProject(event.project_id);
  }
  if (viewing(event.project_id)) return;
  const title = store.titles[event.project_id] ?? "营销片";
  const action = { label: "查看", onClick: () => openProject(event.project_id) };
  switch (roundTransition(prev, round)) {
    case "ready":
      toast.success(`「${title}」${round.scripts.length} 份脚本已写好`, { action });
      break;
    case "failed":
      toast.error(`「${title}」写脚本失败，积分已退回`, { action });
      break;
  }
}

/**
 * 收到 marketing.script：并进所在轮次；出片成功且用户不在该项目页面时提醒，出片失败不提醒（原因在舞台上看）。
 * 本地缺轮次时重新拉详情。
 */
export function handleMarketingScriptEvent(event: MarketingScriptEvent) {
  const store = useMarketingStore.getState();
  const script = mapScript(event.script);
  const prev = store.projects[event.project_id]?.rounds
    .find((item) => item.id === event.round_id)
    ?.scripts.find((item) => item.id === script.id);
  if (!store.applyScript(event.project_id, event.round_id, script)) {
    if (store.watching[event.project_id]) void reloadMarketingProject(event.project_id);
  }
  if (scriptNotice(prev, script, viewing(event.project_id)) === "done") {
    const title = store.titles[event.project_id] ?? "营销片";
    const action = { label: "查看", onClick: () => openProject(event.project_id) };
    toast.success(`「${title}」有一条成片已出好`, { action });
  }
}

/** WebSocket 重连 / 页面回到前台时，对账所有正在看的项目详情（断线期间的变化在这里补上），并让列表刷新 */
export async function reconcileWatchedMarketing() {
  const store = useMarketingStore.getState();
  store.bumpList();
  await Promise.all(Object.keys(store.watching).map((id) => reloadMarketingProject(id)));
}
