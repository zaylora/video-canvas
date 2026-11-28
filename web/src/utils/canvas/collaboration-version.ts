export type CanvasVersion = number | string;

const maxVersion = 18_446_744_073_709_551_615n;
const versionFields = new Set([
  "epoch",
  "collaboration_epoch",
  "revision",
  "baseRevision",
  "expectedRevision",
  "cursor",
  "recordBase",
  "previousRevision",
  "undoes",
  "runRevision",
  "generationRevision",
  "revision_before",
  "revision_after",
]);
const jsonTokens = /"(?:\\[\s\S]|[^"\\])*"|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?/g;

export function isPositiveCanvasVersion(value: unknown): value is CanvasVersion {
  if (typeof value === "number") return Number.isSafeInteger(value) && value > 0;
  return (
    typeof value === "string" &&
    /^[1-9]\d{0,19}$/.test(value) &&
    !Number.isSafeInteger(Number(value)) &&
    BigInt(value) <= maxVersion
  );
}

export function compareCanvasVersions(left: CanvasVersion, right: CanvasVersion): number {
  if (typeof left === "number" && typeof right === "number") return left - right;
  const first = BigInt(left);
  const second = BigInt(right);
  return first === second ? 0 : first < second ? -1 : 1;
}

export function maxCanvasVersion(left: CanvasVersion, right: CanvasVersion): CanvasVersion {
  return compareCanvasVersions(left, right) >= 0 ? left : right;
}

export function nextCanvasVersion(version: CanvasVersion): CanvasVersion {
  if (typeof version === "number" && version < Number.MAX_SAFE_INTEGER) return version + 1;
  return String(BigInt(version) + 1n);
}

export function parseCanvasVersions(text: string): unknown {
  return JSON.parse(
    text.replace(jsonTokens, (token) => {
      if (/^[1-9]\d{15,19}$/.test(token) && !Number.isSafeInteger(Number(token))) {
        return `"${token}"`;
      }
      return token;
    }),
  );
}

export function stringifyCanvasVersions(body: object): string {
  const original = JSON.stringify(body);
  if (!/"[1-9]\d{15,19}"/.test(original)) return original;
  let marker = "__vc_version_";
  while (original.includes(marker)) marker += "_";
  const protocolObjects = new WeakSet<object>([body]);
  const fieldBases = new WeakSet<object>();
  const marked = JSON.stringify(body, function (this: object, key, value: unknown) {
    if (protocolObjects.has(this) && value && typeof value === "object") {
      if (key === "fieldBases") fieldBases.add(value);
      if (key === "collaboration" || (key === "data" && this === body && "type" in body)) {
        protocolObjects.add(value);
      }
      if (key === "changes" && Array.isArray(value)) {
        for (const change of value) {
          if (change && typeof change === "object") protocolObjects.add(change);
        }
      }
    }
    if (
      ((protocolObjects.has(this) && versionFields.has(key)) || fieldBases.has(this)) &&
      typeof value === "string" &&
      isPositiveCanvasVersion(value)
    ) {
      return `${marker}${value}`;
    }
    return value;
  });
  return marked.replace(new RegExp(`"${marker}([0-9]+)"`, "g"), "$1");
}
