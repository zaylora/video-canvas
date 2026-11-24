import { FormField } from "@/components/admin-ui/form-field";
import { ToggleChip } from "@/components/admin-ui/toggle-chip";
import {
  MCP_SCOPES,
  MCP_SCOPE_GROUPS,
  dropMCPScope,
  expandMCPScopes,
  scopeRequiresLabel,
} from "@/utils/admin/mcp-project";

/**
 * 能力勾选：新建与编辑共用。
 *
 * 勾上会补全它依赖的档位，取消会连带关掉依赖它的档位——权限之间不互斥，
 * 界面上不会留下「给了写却没给读」这种配了也用不了的组合。
 *
 * 画布与短剧是两条并列的能力域，分行摆：MCP 是独立的能力出口，不是「画布的子功能」。
 * 用 ToggleChip 而不是复选框加长说明，和后台其它「能力」多选一致（coding-standards 6.2）。
 *
 * @param scopes 当前勾选的档位
 * @param onChange 勾选结果变化时回调（已经补全依赖）
 */
/** 胶囊悬停说明：一句话讲清这一档做什么，有前置依赖就顺带写上。 */
const chipTitle = (value: string, hint: string) => {
  const requires = scopeRequiresLabel(value);
  return requires ? `${hint} · 需要「${requires}」` : hint;
};

export function ScopeChecklist({
  scopes,
  onChange,
}: {
  scopes: string[];
  onChange: (next: string[]) => void;
}) {
  /** 点一下切换：勾上补依赖，取消连带收掉依赖它的档位。 */
  const toggle = (value: string) =>
    onChange(
      scopes.includes(value) ? dropMCPScope(scopes, value) : expandMCPScopes([...scopes, value]),
    );
  return (
    <FormField label="能力" tip="勾上会自动带上它依赖的档位，取消会连带关掉依赖它的档位">
      <div className="grid gap-2">
        {MCP_SCOPE_GROUPS.map((group) => (
          <div
            key={group.key}
            role="group"
            aria-label={`${group.label}能力`}
            className="flex flex-wrap items-center gap-2"
          >
            <span className="text-muted-foreground w-8 shrink-0 text-xs">{group.label}</span>
            {MCP_SCOPES.filter((scope) => scope.group === group.key).map((scope) => (
              <ToggleChip
                key={scope.value}
                pressed={scopes.includes(scope.value)}
                title={chipTitle(scope.value, scope.hint)}
                onClick={() => toggle(scope.value)}
              >
                {scope.label}
              </ToggleChip>
            ))}
          </div>
        ))}
      </div>
    </FormField>
  );
}
