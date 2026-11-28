import { APIError, encodeUnsignedInteger } from "../client.ts";
import type { Flags, WireVersion } from "../types.ts";

export function isWireVersion(value: unknown, allowZero = false): value is WireVersion {
  if (typeof value === "number") return Number.isSafeInteger(value) && value >= (allowZero ? 0 : 1);
  return (
    typeof value === "string" &&
    (allowZero ? /^(0|[1-9]\d{0,19})$/ : /^[1-9]\d{0,19}$/).test(value) &&
    BigInt(value) <= 18_446_744_073_709_551_615n
  );
}

export function versionFlag(
  flags: Flags,
  name: string,
  allowZero = false,
): WireVersion | undefined {
  const value = flags[name];
  if (value === undefined) return undefined;
  if (!isWireVersion(value, allowZero)) throw new APIError(-1, `--${name} 需要有效的版本整数`, 2);
  const numeric = Number(value);
  return Number.isSafeInteger(numeric) ? numeric : value;
}

export function compareVersions(left: WireVersion, right: WireVersion): number {
  const difference = BigInt(left) - BigInt(right);
  return difference === 0n ? 0 : difference > 0n ? 1 : -1;
}

export function encodeWireVersion(value: WireVersion): number | object {
  if (!isWireVersion(value, true)) throw new APIError(-1, "版本整数无效", 2);
  if (value === "0" || typeof value === "number") return Number(value);
  return encodeUnsignedInteger(value);
}

const versionFields = new Set([
  "epoch",
  "revision",
  "baseRevision",
  "expectedRevision",
  "recordBase",
  "collaboration_epoch",
]);

export function encodeCollaborationVersions(body: unknown): unknown {
  if (!body || typeof body !== "object" || Array.isArray(body)) return body;
  return Object.fromEntries(
    Object.entries(body).map(([key, value]: [string, unknown]) => {
      if (versionFields.has(key) && value !== undefined)
        return [key, encodeWireVersion(value as WireVersion)];
      if (key === "collaboration") return [key, encodeCollaborationVersions(value)];
      if (key === "changes" && Array.isArray(value))
        return [key, value.map(encodeCollaborationVersions)];
      if (key === "fieldBases" && value && typeof value === "object")
        return [
          key,
          Object.fromEntries(
            Object.entries(value).map(([field, version]) => [field, encodeWireVersion(version)]),
          ),
        ];
      return [key, value];
    }),
  );
}
