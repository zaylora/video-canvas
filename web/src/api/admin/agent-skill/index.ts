import service from "@/utils/requests/service";
import { adminAgentSkillEndpoints as ep } from "./endpoints";
import type {
  SkillDeleteCheck,
  SkillDetail,
  SkillFileContent,
  SkillImportInput,
  SkillImportView,
  SkillItem,
  SkillStatusFilter,
  SkillVersionView,
} from "./type";

/** 后端可能把空数组序列化成 null：列表类字段统一补成 [] */
const normalizeItem = (item: SkillItem): SkillItem => ({
  ...item,
  unsupported_fields: item.unsupported_fields ?? [],
});

const normalizeImport = (view: SkillImportView): SkillImportView => ({
  ...view,
  frontmatter: view.frontmatter ?? {},
  unsupported_fields: view.unsupported_fields ?? [],
  files: view.files ?? [],
  issues: view.issues ?? [],
});

/**
 * 技能列表（含只读内置）；后端可能返回 null，统一补成空数组
 * @param params q 按名称 / 显示名 / 说明搜索；status 只看已启用或已停用
 * @returns 技能列表
 */
export const listSkills = async (params?: {
  q?: string;
  status?: SkillStatusFilter;
}): Promise<SkillItem[]> => {
  const list = await service.get<SkillItem[] | null>(ep.list(), params);
  return (list ?? []).map(normalizeItem);
};

/**
 * 技能详情，含版本列表（内置技能带正文、没有版本）
 * @param name 技能名
 * @returns 技能详情
 */
export const getSkill = async (name: string): Promise<SkillDetail> => {
  const detail = await service.get<SkillDetail>(ep.detail(name), undefined);
  return {
    ...normalizeItem(detail),
    versions: (detail.versions ?? []).map((v) => ({
      ...v,
      unsupported_fields: v.unsupported_fields ?? [],
    })),
    body: detail.body,
  };
};

/**
 * 某个版本的完整信息：frontmatter、正文、文件清单、预检问题
 * @param name 技能名
 * @param version 版本号
 * @returns 版本详情
 */
export const getSkillVersion = async (name: string, version: number): Promise<SkillVersionView> => {
  const view = await service.get<SkillVersionView>(ep.version(name, version), undefined);
  return {
    ...view,
    unsupported_fields: view.unsupported_fields ?? [],
    frontmatter: view.frontmatter ?? {},
    files: view.files ?? [],
    issues: view.issues ?? [],
  };
};

/**
 * 读某版本包内的一个文件：文本给内容，二进制只给大小
 * @param name 技能名
 * @param version 版本号
 * @param path 包内路径
 * @returns 文件内容
 */
export const getSkillFile = (name: string, version: number, path: string) =>
  service.get<SkillFileContent>(ep.versionFile(name, version), { path });

/**
 * 下载某版本的规范化整包。要带登录凭证，所以走请求实例取 blob，由调用方保存成文件
 * @param name 技能名
 * @param version 版本号
 * @returns zip 内容
 */
export const downloadSkillVersion = (name: string, version: number) =>
  service.request<Blob>({ url: ep.versionDownload(name, version), responseType: "blob" });

/**
 * 把选好的包拼成 multipart：zip 用字段 file；文件夹 / 单个 SKILL.md 用成对的 files 与 paths，
 * paths 是含外层目录的相对路径。files 与 paths 必须逐文件交替追加，后端按出现顺序配对
 * @param input 选好的包
 * @returns 请求体
 */
export function buildImportForm(input: SkillImportInput): FormData {
  const form = new FormData();
  if (input.kind === "zip") {
    form.append("file", input.file, input.file.name);
    return form;
  }
  for (const entry of input.entries) {
    form.append("files", entry.file, entry.file.name);
    form.append("paths", entry.path);
  }
  return form;
}

/**
 * 上传并预检。预检不通过也返回 200，问题在 issues 里；id 为空串表示包完全不可读、没有暂存
 * @param input 选好的包
 * @param options onProgress 上传进度（0 到 1）；signal 用来取消上传
 * @returns 预检结果
 */
export const importSkill = async (
  input: SkillImportInput,
  options?: { onProgress?: (ratio: number) => void; signal?: AbortSignal },
): Promise<SkillImportView> => {
  const view = await service.upload<SkillImportView>(ep.imports(), buildImportForm(input), {
    signal: options?.signal,
    // 50 MB 的包在慢网络上可能要几分钟，放宽默认的 2 分钟
    timeout: 10 * 60 * 1000,
    onUploadProgress: (event) => {
      if (event.total) options?.onProgress?.(event.loaded / event.total);
    },
  });
  return normalizeImport(view);
};

/**
 * 预览暂存包里的一个文件（确认前）
 * @param id 暂存 id
 * @param path 包内路径
 * @returns 文件内容
 */
export const getImportFile = (id: string, path: string) =>
  service.get<SkillFileContent>(ep.importFile(id), { path });

/**
 * 放弃暂存的导入包。属于后台清理，失败不弹全局 toast（暂存 1 小时后也会被清理任务回收）
 * @param id 暂存 id
 */
export const discardImport = (id: string) =>
  service.delete<void>(ep.importDiscard(id), { silent: true });

/**
 * 确认导入：新技能默认停用；同名技能新增版本但生效版本不变
 * @param id 暂存 id
 * @returns 入库后的技能
 */
export const confirmImport = async (id: string): Promise<SkillItem> =>
  normalizeItem(await service.post<SkillItem>(ep.importConfirm(id), undefined));

/**
 * 改显示名（内置技能不可改）
 * @param name 技能名
 * @param title 新显示名
 * @returns 更新后的技能
 */
export const renameSkill = async (name: string, title: string): Promise<SkillItem> =>
  normalizeItem(await service.put<SkillItem>(ep.rename(name), { title }));

/**
 * 启用 / 停用
 * @param name 技能名
 * @param enabled 是否启用
 * @returns 更新后的技能
 */
export const setSkillEnabled = async (name: string, enabled: boolean): Promise<SkillItem> =>
  normalizeItem(await service.put<SkillItem>(ep.enabled(name), { enabled }));

/**
 * 设为生效版本（回滚就是把旧版本设为生效）；已启用时只影响之后新开始的 Agent 运行
 * @param name 技能名
 * @param version 版本号
 * @returns 更新后的技能
 */
export const setSkillActiveVersion = async (name: string, version: number): Promise<SkillItem> =>
  normalizeItem(await service.put<SkillItem>(ep.activeVersion(name), { version }));

/**
 * 删除预检：能不能删、会删掉几个版本
 * @param name 技能名
 * @returns 预检结果
 */
export const getSkillDeleteCheck = (name: string) =>
  service.get<SkillDeleteCheck>(ep.deleteCheck(name), undefined);

/**
 * 删除一个版本（生效版本不能删）；同时删掉对象存储里的整包，不可恢复
 * @param name 技能名
 * @param version 版本号
 */
export const deleteSkillVersion = (name: string, version: number) =>
  service.delete<void>(ep.version(name, version));

/**
 * 删除技能（必须先停用）；删除全部版本和整包，不可恢复
 * @param name 技能名
 */
export const deleteSkill = (name: string) => service.delete<void>(ep.remove(name));
