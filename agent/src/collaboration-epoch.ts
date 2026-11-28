export type CollaborationEpoch = number | string;

const MAX_EPOCH = (1n << 63n) - 1n;
const exactJSON = JSON as unknown as {
  parse(
    text: string,
    reviver: (key: string, value: unknown, context?: { source?: string }) => unknown,
  ): unknown;
  rawJSON(text: string): object;
};

export function normalizeEpoch(value: unknown): CollaborationEpoch {
  if (typeof value === "number" && Number.isSafeInteger(value) && value > 0) return value;
  if (typeof value === "string" && /^[1-9]\d{0,18}$/.test(value) && BigInt(value) <= MAX_EPOCH) {
    const numeric = Number(value);
    return Number.isSafeInteger(numeric) ? numeric : value;
  }
  throw new Error("协作代数必须是有效的正整数");
}

export function parseEpochJSON(text: string): unknown {
  return exactJSON.parse(text, (key, value, context) => {
    if (typeof value !== "number" || !context?.source) return value;
    if (key === "collaboration_epoch" && !/^[1-9]\d*$/.test(context.source)) return context.source;
    if (
      ["collaboration_epoch", "revision", "revision_before", "revision_after"].includes(key) &&
      /^[1-9]\d*$/.test(context.source) &&
      !Number.isSafeInteger(value)
    )
      return context.source;
    return value;
  });
}

export function encodeEpoch(value: CollaborationEpoch): number | object {
  const epoch = normalizeEpoch(value);
  return typeof epoch === "number" ? epoch : exactJSON.rawJSON(epoch);
}
