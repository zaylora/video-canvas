import { expect, spyOn, test } from "bun:test";

import { installDom } from "@/tests/helpers/dom";
import { ApiError } from "@/utils/requests/request";
import service from "@/utils/requests/service";

test("节点评论可提及当前成员，提交失败后重试使用相同请求", async () => {
  const dom = await installDom();
  const { CanvasCommentThread } = await import("@/pages/canvas/collaboration-comments");
  const comment = {
    id: "comment",
    authorId: "abc1",
    authorUsername: "小林",
    body: "验收镜头",
    mentions: ["abc2"],
  };
  const get = spyOn(service, "get").mockResolvedValue({
    comments: [{ ...comment, id: "older", authorId: "abc3", authorUsername: undefined }],
    members: ["abc1", "abc2"],
    usernames: { abc1: "小林", abc2: "小周", abc3: "小何" },
  });
  const post = spyOn(service, "post")
    .mockRejectedValueOnce(new ApiError("连接中断", "HTTP_503", 503))
    .mockResolvedValue(comment);
  const view = await dom.mount(<CanvasCommentThread canvasId="c1" nodeId="n1" epoch={3} />);
  try {
    await dom.type(view.container.querySelector("textarea"), "验收镜头");
    expect(view.container.querySelector("article")?.textContent).toContain("小何");
    await dom.click(view.container.querySelector('[aria-label="提及小周"]'));
    const submit = view.container.querySelector('button[type="submit"]');
    await dom.click(submit);
    expect(view.container.querySelector('[role="alert"]')?.textContent).toContain("连接中断");
    await dom.click(submit);
    expect(post.mock.calls).toHaveLength(2);
    expect(post.mock.calls[0]?.[1]).toEqual(post.mock.calls[1]?.[1]);
    expect(post.mock.calls[1]?.[1]).toMatchObject({
      body: "验收镜头",
      mentions: ["abc2"],
      nodeId: "n1",
      epoch: 3,
    });
    expect(view.container.textContent).toContain("验收镜头");
    expect(view.container.querySelector("article")?.textContent).toContain("小林");
    expect(view.container.querySelector("article")?.textContent).toContain("@小周");
    expect(view.container.textContent).not.toMatch(/abc[123]/);
  } finally {
    await view.unmount();
    post.mockRestore();
    get.mockRestore();
    dom.restore();
  }
});

test("旧轮询不能覆盖刚确认的评论，撤权会中止在途提交", async () => {
  const dom = await installDom();
  const { CanvasCommentThread } = await import("@/pages/canvas/collaboration-comments");
  const empty = { comments: [], members: [] };
  const poll = Promise.withResolvers<typeof empty>();
  const pending = Promise.withResolvers<unknown>();
  const get = spyOn(service, "get")
    .mockResolvedValueOnce(empty)
    .mockReturnValueOnce(poll.promise)
    .mockRejectedValue(new ApiError("已撤权", "HTTP_403", 403));
  const post = spyOn(service, "post")
    .mockResolvedValueOnce({ id: "ok", authorId: "abc1", body: "刚确认" })
    .mockReturnValueOnce(pending.promise);
  const view = await dom.mount(<CanvasCommentThread canvasId="c1" nodeId="n1" epoch={3} />);
  const foreground = () =>
    dom.act(async () => {
      dom.win.document.dispatchEvent(new dom.win.Event("visibilitychange"));
    });
  const submit = () => dom.click(view.container.querySelector('button[type="submit"]'));
  const typeComment = (value: string) => dom.type(view.container.querySelector("textarea"), value);
  try {
    await foreground();
    expect(get.mock.calls).toHaveLength(2);
    await typeComment("刚确认");
    await submit();
    await dom.act(async () => poll.resolve(empty));
    expect(view.container.textContent).toContain("刚确认");
    await typeComment("在途评论");
    await submit();
    await foreground();
    expect(post.mock.calls[1]?.[2]?.signal?.aborted).toBe(true);
    await dom.act(async () => pending.resolve({ id: "pending", body: "在途评论" }));
    expect(view.container.textContent).toContain("讨论已不可访问");
    expect(view.container.textContent).not.toContain("刚确认");
    expect(view.container.textContent).not.toContain("在途评论");
  } finally {
    await view.unmount();
    get.mockRestore();
    post.mockRestore();
    dom.restore();
  }
});

test.each([true, false])("评论轮询与旧页同时返回时保持连续顺序，存在交集 %s", async (overlap) => {
  const dom = await installDom();
  const { CanvasCommentThread } = await import("@/pages/canvas/collaboration-comments");
  const page = (ids: string[], before?: string) => ({
    comments: ids.map((id) => ({ id, authorId: "abc1", body: id, createdAt: "" })),
    members: ["abc1"],
    before,
  });
  const older = Promise.withResolvers<ReturnType<typeof page>>();
  const get = spyOn(service, "get")
    .mockResolvedValueOnce(page(["第三条", "第二条"], "第二条"))
    .mockReturnValueOnce(older.promise)
    .mockResolvedValueOnce(
      overlap ? page(["第四条", "第三条"], "第三条") : page(["第六条", "第五条"], "第五条"),
    )
    .mockResolvedValueOnce(page(["第四条", "第三条", "第二条", "第一条"]));
  const view = await dom.mount(<CanvasCommentThread canvasId="c1" nodeId="n1" epoch={3} />);
  const loadOlder = () =>
    dom.click(
      [...view.container.querySelectorAll("button")].find((button) =>
        button.textContent?.includes("加载更早的评论"),
      ),
    );
  const comments = () =>
    [...view.container.querySelectorAll("article p")].map((note) => note.textContent);
  try {
    await loadOlder();
    expect(get.mock.calls[1]?.[1]).toMatchObject({ before: "第二条" });
    await dom.act(async () => document.dispatchEvent(new Event("visibilitychange")));
    await dom.act(async () => older.resolve(page(["第一条"])));
    if (overlap) {
      expect(comments()).toEqual(["第四条", "第三条", "第二条", "第一条"]);
    } else {
      expect(comments()).toEqual(["第六条", "第五条"]);
      await loadOlder();
      expect(get.mock.calls[3]?.[1]).toMatchObject({ before: "第五条" });
      expect(comments()).toEqual(["第六条", "第五条", "第四条", "第三条", "第二条", "第一条"]);
    }
  } finally {
    await view.unmount();
    get.mockRestore();
    dom.restore();
  }
});
