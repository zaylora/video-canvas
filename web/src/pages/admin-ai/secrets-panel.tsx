import { useCallback, useEffect, useState } from "react";
import { CheckCircle2, CircleDashed, Loader2, RefreshCw } from "lucide-react";

import { listSecrets, setSecret } from "@/api/admin-ai";
import type { SecretStatus } from "@/api/admin-ai/type";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { toast } from "sonner";

/** RunningHub 凭证的固定名字（平台配置里 auth.secret 引用它） */
const KNOWN_SECRETS = ["runninghub_api_key"];

/**
 * 凭证设置：只写。这里只能看到「是否已设置」和更新时间，
 * 输入框提交后立刻清空，明文不会回显也读不回来。
 */
export function SecretsPanel() {
  const [items, setItems] = useState<SecretStatus[]>([]);
  const [loading, setLoading] = useState(true);
  const [name, setName] = useState(KNOWN_SECRETS[0]);
  const [value, setValue] = useState("");
  const [saving, setSaving] = useState(false);

  const reload = useCallback(async () => {
    setLoading(true);
    try {
      setItems(await listSecrets());
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  const save = async () => {
    const trimmedName = name.trim();
    if (!trimmedName || !value) return;
    setSaving(true);
    try {
      await setSecret(trimmedName, value);
      setValue("");
      toast.success(`凭证 ${trimmedName} 已更新`);
      void reload();
    } finally {
      setSaving(false);
    }
  };

  // 已知凭证即使后端列表里还没有，也占一行，方便直接去设置
  const names = [...new Set([...KNOWN_SECRETS, ...items.map((item) => item.name)])];

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-6 p-6">
      <section className="flex flex-col gap-3">
        <div className="flex items-center justify-between">
          <h2 className="text-base font-medium">凭证状态</h2>
          <Button variant="ghost" size="sm" onClick={() => void reload()} disabled={loading}>
            {loading ? <Loader2 className="animate-spin" /> : <RefreshCw />}
            刷新
          </Button>
        </div>
        <ul className="divide-y rounded-xl border">
          {names.map((secretName) => {
            const status = items.find((item) => item.name === secretName);
            return (
              <li key={secretName} className="flex items-center gap-3 px-4 py-3 text-sm">
                {status?.is_set ? (
                  <CheckCircle2 className="size-4 text-emerald-600 dark:text-emerald-400" />
                ) : (
                  <CircleDashed className="text-muted-foreground size-4" />
                )}
                <div className="min-w-0 flex-1">
                  <p className="font-mono text-xs">{secretName}</p>
                  <p className="text-muted-foreground text-xs">
                    {status?.is_set
                      ? `已设置${status.updated_at ? ` · 更新于 ${new Date(status.updated_at).toLocaleString()}` : ""}`
                      : "尚未设置"}
                    {status?.referenced_by?.length
                      ? ` · 被平台引用：${status.referenced_by.join("、")}`
                      : ""}
                  </p>
                </div>
                <Button variant="outline" size="xs" onClick={() => setName(secretName)}>
                  设置
                </Button>
              </li>
            );
          })}
        </ul>
      </section>

      <section className="flex flex-col gap-3">
        <h2 className="text-base font-medium">设置凭证</h2>
        <p className="text-muted-foreground text-xs">
          只写：保存后任何界面都读不回明文；重复设置会覆盖旧值，并立即对进行中的任务生效。
        </p>
        <div className="grid gap-3 sm:grid-cols-[14rem_1fr_auto]">
          <Input
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="凭证名"
            aria-label="凭证名"
            className="font-mono"
          />
          <Input
            type="password"
            autoComplete="new-password"
            value={value}
            onChange={(event) => setValue(event.target.value)}
            placeholder="粘贴凭证内容（如 API Key）"
            aria-label="凭证内容"
          />
          <Button disabled={saving || !name.trim() || !value} onClick={() => void save()}>
            {saving && <Loader2 className="animate-spin" />}
            保存
          </Button>
        </div>
      </section>
    </div>
  );
}
