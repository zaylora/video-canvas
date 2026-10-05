import service from "@/utils/requests/service";
import type { RegisterSettings, SmtpSettings, SmtpUpdateRequest } from "./type";

/**
 * 读取注册设置（admin 可读）
 * @returns 开放注册开关、新用户初始积分、默认并发上限
 */
export const getRegisterSettings = () =>
  service.get<RegisterSettings>("/admin/settings/register", undefined);

/**
 * 保存注册设置（super_admin）
 * @param body 开关、初始积分（>=0）、默认并发上限（1 到 64）
 * @returns 保存后的设置
 */
export const updateRegisterSettings = (body: RegisterSettings) =>
  service.put<RegisterSettings>("/admin/settings/register", body);

/**
 * 读取 SMTP 设置（admin 可读）；密码永不返回
 * @returns SMTP 视图，含是否已设置密码与最近一次测试结果
 */
export const getSmtpSettings = () => service.get<SmtpSettings>("/admin/settings/smtp", undefined);

/**
 * 保存 SMTP 设置（super_admin）；后端会拒绝指向内网的主机
 * @param body 除密码外的 SMTP 字段
 * @returns 保存后的 SMTP 视图
 */
export const updateSmtpSettings = (body: SmtpUpdateRequest) =>
  service.put<SmtpSettings>("/admin/settings/smtp", body);

/**
 * 替换 SMTP 密码（super_admin）：只写不读，明文只在请求体里出现一次
 * @param body 新密码
 */
export const replaceSmtpPassword = (body: { password: string }) =>
  service.put<null>("/admin/settings/smtp/password", body);

/**
 * 用已保存的配置发一封测试邮件（super_admin），并更新最近一次测试结果
 * @param body 收件邮箱
 */
export const sendSmtpTest = (body: { to: string }) =>
  service.post<null>("/admin/settings/smtp/test", body);
