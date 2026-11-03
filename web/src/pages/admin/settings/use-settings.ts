import { useAdminConfigChanged } from "@/hooks/use-admin-config-changed";
import { useCallback, useEffect, useState } from "react";

import { getPricingSettings, getRegisterSettings, getSmtpSettings } from "@/api/admin/settings";
import type {
  PricingSettingsView,
  RegisterSettings,
  SmtpSettings,
} from "@/api/admin/settings/type.d";

import { useAliveRef, type LoadStatus } from "../use-admin";

/**
 * 加载一份设置：进页面请求一次；reload 失败时保留上一次的数据。
 * 请求错误的全局提示由拦截器负责，这里只记状态。
 * @param fetcher 读取接口
 * @returns 数据、加载状态、刷新与直接替换数据的方法
 */
function useSettings<T>(fetcher: () => Promise<T>) {
  const aliveRef = useAliveRef();
  const [data, setData] = useState<T | null>(null);
  const [status, setStatus] = useState<LoadStatus>("loading");

  const reload = useCallback(async () => {
    try {
      const value = await fetcher();
      if (!aliveRef.current) return;
      setData(value);
      setStatus("ready");
    } catch {
      if (aliveRef.current) setStatus((prev) => (prev === "ready" ? prev : "error"));
    }
  }, [aliveRef, fetcher]);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { data, status, reload, setData };
}

/** 注册设置 */
export const useRegisterSettings = () => {
  const settings = useSettings<RegisterSettings>(getRegisterSettings);
  useAdminConfigChanged("register_settings", () => void settings.reload());
  return settings;
};

/** SMTP 设置（密码永不返回） */
export const useSmtpSettings = () => {
  const settings = useSettings<SmtpSettings>(getSmtpSettings);
  useAdminConfigChanged("smtp_settings", () => void settings.reload());
  return settings;
};

/** 定价设置（积分换算、默认加价率、汇率覆盖与当前汇率） */
export const usePricingSettings = () => useSettings<PricingSettingsView>(getPricingSettings);
