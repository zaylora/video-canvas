import { describe, expect, test } from "bun:test";

import type { UploadIntent } from "@/api/asset/type";
import {
  buildDirectRequest,
  buildIntentBody,
  planUpload,
  sendDirect,
} from "@/utils/asset/direct-upload";

const png = () => new File(["png-bytes"], "a.png", { type: "image/png" });

describe("buildIntentBody", () => {
  test("带文件名、大小与类型", () => {
    expect(buildIntentBody(png())).toEqual({
      file_name: "a.png",
      size: 9,
      mime_type: "image/png",
    });
  });

  test("文件类型为空时不申请直传（后端要求类型在白名单内）", () => {
    expect(buildIntentBody(new File(["x"], "noext"))).toBeNull();
  });

  test("空文件不申请直传", () => {
    expect(buildIntentBody(new File([], "a.png", { type: "image/png" }))).toBeNull();
  });
});

describe("planUpload：决定走哪条路", () => {
  test("mode=proxy 走中转", () => {
    expect(planUpload({ mode: "proxy" }, png())).toEqual({ kind: "proxy" });
  });

  test("接口失败（没有响应）走中转", () => {
    expect(planUpload(null, png())).toEqual({ kind: "proxy" });
    expect(planUpload(undefined, png())).toEqual({ kind: "proxy" });
  });

  test("direct 但缺关键信息时走中转，不抛错", () => {
    expect(planUpload({ mode: "direct", method: "put", url: "https://x" }, png())).toEqual({
      kind: "proxy",
    });
    expect(planUpload({ mode: "direct", intent_id: 1, method: "put" }, png())).toEqual({
      kind: "proxy",
    });
    expect(planUpload({ mode: "direct", intent_id: 1, url: "https://x" }, png())).toEqual({
      kind: "proxy",
    });
    expect(
      planUpload(
        { mode: "direct", intent_id: 1, url: "https://x", method: "delete" as never },
        png(),
      ),
    ).toEqual({ kind: "proxy" });
  });

  test("direct 且信息完整：给出意图 ID 与请求", () => {
    const plan = planUpload(
      {
        mode: "direct",
        intent_id: 9,
        method: "put",
        url: "https://b.s3/k",
        headers: { "Content-Type": "image/png" },
      },
      png(),
    );
    expect(plan.kind).toBe("direct");
    if (plan.kind === "direct") {
      expect(plan.intentId).toBe(9);
      expect(plan.request.url).toBe("https://b.s3/k");
    }
  });
});

describe("buildDirectRequest", () => {
  test("POST：fields 全部先进表单，file 字段最后一个", () => {
    const file = png();
    const intent: UploadIntent = {
      mode: "direct",
      intent_id: 1,
      method: "post",
      url: "https://b.s3.example.com/",
      fields: { key: "u1/202610/x.png", policy: "p", "x-amz-signature": "s" },
    };
    const request = buildDirectRequest(intent, file);
    expect(request?.url).toBe("https://b.s3.example.com/");
    expect(request?.init.method).toBe("POST");
    expect(request?.init.headers).toBeUndefined();
    const body = request?.init.body as FormData;
    expect([...body.keys()]).toEqual(["key", "policy", "x-amz-signature", "file"]);
    expect(body.get("key")).toBe("u1/202610/x.png");
    expect((body.get("file") as File).name).toBe("a.png");
  });

  test("POST：fields 里就算有名为 file 的键也不会盖掉真正的文件，file 仍只有一份且在最后", () => {
    const request = buildDirectRequest(
      {
        mode: "direct",
        intent_id: 1,
        method: "post",
        url: "https://x",
        fields: { file: "evil", key: "k" },
      },
      png(),
    );
    const body = request?.init.body as FormData;
    expect([...body.keys()]).toEqual(["key", "file"]);
    expect(body.get("file")).toBeInstanceOf(File);
  });

  test("POST：没有 fields 也能构造", () => {
    const request = buildDirectRequest(
      { mode: "direct", intent_id: 1, method: "post", url: "https://x" },
      png(),
    );
    const body = request?.init.body as FormData;
    expect([...body.keys()]).toEqual(["file"]);
  });

  test("PUT：直接把文件当 body，带上签名要求的请求头", () => {
    const file = png();
    const request = buildDirectRequest(
      {
        mode: "direct",
        intent_id: 2,
        method: "put",
        url: "https://acct.r2.cloudflarestorage.com/b/k?X-Amz-Signature=s",
        headers: { "Content-Type": "image/png" },
      },
      file,
    );
    expect(request?.init.method).toBe("PUT");
    expect(request?.init.body).toBe(file);
    expect(request?.init.headers).toEqual({ "Content-Type": "image/png" });
  });

  test("PUT：浏览器不允许脚本设置 Content-Length，签名时带上的这个头要去掉，其余保留", () => {
    const request = buildDirectRequest(
      {
        mode: "direct",
        intent_id: 2,
        method: "put",
        url: "https://x",
        headers: { "Content-Type": "image/png", "Content-Length": "9", "content-length": "9" },
      },
      png(),
    );
    expect(request?.init.headers).toEqual({ "Content-Type": "image/png" });
  });

  test("缺 url 或方法不对时返回 null", () => {
    expect(buildDirectRequest({ mode: "direct", intent_id: 1, method: "put" }, png())).toBeNull();
    expect(
      buildDirectRequest({ mode: "direct", intent_id: 1, url: "https://x" }, png()),
    ).toBeNull();
  });
});

describe("sendDirect：直传结果判定（注入 fetch，不发真实网络请求）", () => {
  const request = { url: "https://x", init: { method: "PUT" as const, body: png() } };

  test("2xx 算成功（S3 的 POST Policy 成功返回 204）", async () => {
    for (const status of [200, 201, 204]) {
      const fake = (async () => new Response(null, { status })) as unknown as typeof fetch;
      expect(await sendDirect(request, fake)).toBe(true);
    }
  });

  test("非 2xx 算失败，需要降级", async () => {
    for (const status of [301, 403, 404, 500]) {
      const fake = (async () => new Response(null, { status })) as unknown as typeof fetch;
      expect(await sendDirect(request, fake)).toBe(false);
    }
  });

  test("fetch 抛错（CORS、断网）算失败，不向外抛", async () => {
    const fake = (async () => {
      throw new TypeError("Failed to fetch");
    }) as unknown as typeof fetch;
    expect(await sendDirect(request, fake)).toBe(false);
  });

  test("原样把 url 与 init 交给 fetch，并且不带登录头、不带 cookie", async () => {
    let seen: { url: unknown; init: RequestInit | undefined } | null = null;
    const fake = (async (url: unknown, init?: RequestInit) => {
      seen = { url, init };
      return new Response(null, { status: 204 });
    }) as unknown as typeof fetch;
    await sendDirect(request, fake);
    expect(seen!.url).toBe("https://x");
    expect(seen!.init?.method).toBe("PUT");
    expect(seen!.init?.credentials).toBe("omit");
    expect(new Headers(seen!.init?.headers).has("authorization")).toBe(false);
  });
});
