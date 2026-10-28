/** 资产页的两个范围（角色卡已改为画布内的节点，不再有资产页范围） */
export type AssetsTab = "history" | "library";

/** 资产页放在地址栏里的状态：刷新、后退都能还原 */
export type AssetsRoute = {
  /** 当前范围，默认生成历史 */
  tab: AssetsTab;
  /** 个人资产库里当前所在的文件夹 ID；null 是根目录，其它范围恒为 null */
  folder: string | null;
};

const TABS: readonly AssetsTab[] = ["history", "library"];

/** 文件夹 ID 是后端的十六进制串，别的值一律不信 */
const FOLDER_ID = /^[0-9a-f]+$/i;

/**
 * 从地址栏参数解析资产页状态：未知范围回到生成历史，非法或非资产库范围下的 folder 当作根目录。
 * @param params 地址栏的 query 参数
 * @returns 资产页状态
 */
export function parseAssetsParams(params: URLSearchParams): AssetsRoute {
  const raw = params.get("tab");
  const tab = TABS.find((item) => item === raw) ?? "history";
  const folder = params.get("folder");
  return {
    tab,
    folder: tab === "library" && folder && FOLDER_ID.test(folder) ? folder : null,
  };
}

/**
 * 把资产页状态写成地址栏的 query 串（不带问号）：默认的生成历史不带参数，
 * folder 只在个人资产库里写。
 * @param route 资产页状态
 * @returns query 串，可能为空串
 */
export function buildAssetsSearch(route: AssetsRoute): string {
  const params = new URLSearchParams();
  if (route.tab !== "history") params.set("tab", route.tab);
  if (route.tab === "library" && route.folder) params.set("folder", route.folder);
  return params.toString();
}
