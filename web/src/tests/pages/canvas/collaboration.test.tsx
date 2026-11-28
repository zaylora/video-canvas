import { describe, expect, spyOn, test } from "bun:test";
import { useEffect } from "react";

import type { CollaborationBatch, CollaborationResult } from "@/api/canvas/collaboration";
import type { CanvasGraphDto } from "@/api/canvas/type";
import { useCanvasCollaboration } from "@/hooks/use-canvas-collaboration";
import { installDom } from "@/tests/helpers/dom";
import { ApiError } from "@/utils/requests/request";
import service from "@/utils/requests/service";
import { getCurrentUserId } from "@/utils/storage/user-id";
import { dispatchCanvasRoomMessage } from "@/utils/ws/canvas-room";
import {
  applyCollaborationChanges,
  collaborationFieldValue,
  diffCollaborationGraph,
} from "@/utils/canvas/collaboration-graph";
import {
  CanvasCollaborationSession,
  canvasCollaborationDraftKey,
  pruneCanvasCollaborationDrafts,
} from "@/utils/canvas/collaboration-session";

const graph = (): CanvasGraphDto => ({
  nodes: [
    {
      id: "first",
      type: "canvas",
      position: { x: 0, y: 0 },
      data: { kind: "image", label: "旧标题", model: "test" },
    },
    {
      id: "second",
      type: "canvas",
      position: { x: 0, y: 0 },
      data: { kind: "video", label: "未改动" },
    },
  ],
  edges: [],
  viewport: { x: 20, y: 10, zoom: 1.3 },
});

const passResponse = <T,>(data: unknown): T => data as T;

describe("画布协作增量", () => {
  test("节点字段增量复用连线数组，连线增量复用节点数组", () => {
    const initial = graph();
    initial.edges.push({ id: "link", source: "first", target: "second" });
    const renamed = applyCollaborationChanges(initial, [
      { kind: "node", id: "first", fields: { "data.label": "新标题" } },
    ]);
    expect(renamed.edges).toBe(initial.edges);
    expect(renamed.nodes[0].data.label).toBe("新标题");

    const unlinked = applyCollaborationChanges(renamed, [
      { kind: "edge", id: "link", action: "delete" },
    ]);
    expect(unlinked.nodes).toBe(renamed.nodes);
    expect(unlinked.edges).toEqual([]);
  });

  test("重复读取乐观图谱复用缓存，并在本地捕获和确认后失效", () => {
    const initial = graph();
    const session = new CanvasCollaborationSession(1, 8, initial);
    const first = session.optimistic;

    expect(session.optimistic).toBe(first);

    const change = { kind: "node" as const, id: "first", fields: { "data.label": "本地标题" } };
    session.capture(initial, applyCollaborationChanges(initial, [change]));
    const pending = session.next();
    const local = session.optimistic;

    expect(local.nodes[0]?.data.label).toBe("本地标题");
    expect(session.optimistic).toBe(local);
    expect(pending).toBeDefined();

    expect(session.apply({ epoch: 1, opId: pending!.opId, revision: 9, changes: [change] })).toBe(
      true,
    );
    expect(session.optimistic).not.toBe(local);
    expect(session.optimistic.nodes[0]?.data.label).toBe("本地标题");
  });

  test("大整数回放按相邻版本连续应用，忽略重复与其他代数的事件", () => {
    const initial = graph();
    const epoch = "9007199254740993";
    const session = new CanvasCollaborationSession(epoch, "9007199254740992", initial);
    const event = {
      epoch,
      revision: "9007199254740993",
      opId: "",
      changes: [{ kind: "node" as const, id: "first", fields: { "data.label": "大版本标题" } }],
    };
    expect(session.apply({ ...event, epoch: "9007199254740992" }, "replay")).toBe(false);
    expect(session.apply({ ...event, revision: "9007199254740994" }, "replay")).toBe(false);
    expect(session.apply(event, "replay")).toBe(true);
    expect(session.apply(event, "replay")).toBe(true);
    expect(session.confirmed.nodes[0]?.data.label).toBe("大版本标题");
    expect(session.revision).toBe(event.revision);
    session.capture(
      session.confirmed,
      applyCollaborationChanges(session.confirmed, [
        { ...event.changes[0], fields: { "data.label": "本地修改" } },
      ]),
    );
    expect(session.next()).toMatchObject({ epoch, baseRevision: event.revision });
  });

  test("无可见改动的回放推进版本，保留确认图和本地视角", () => {
    const initial = graph();
    const session = new CanvasCollaborationSession(1, 8, initial);
    expect(session.apply({ epoch: 1, revision: 9, opId: "" }, "replay")).toBe(true);
    expect(session.confirmed).toBe(initial);
    expect(session.revision).toBe(9);

    const change = { kind: "node" as const, id: "first", fields: { "data.label": "新标题" } };
    expect(session.apply({ epoch: 1, revision: 11, opId: "", changes: [change] }, "replay")).toBe(
      false,
    );
    expect(session.apply({ epoch: 1, revision: 10, opId: "", changes: [change] }, "replay")).toBe(
      true,
    );
    expect(session.confirmed.nodes[0]?.data.label).toBe("新标题");
    expect(session.confirmed.viewport).toBe(initial.viewport);
  });

  test("无可见变更的对账推进版本，但不重新投影已显示的画布", async () => {
    const dom = await installDom();
    const initial = graph();
    const snapshot = {
      epoch: 1,
      role: "owner" as const,
      canvas: {
        id: "quiet-canvas",
        title: "协作",
        description: null,
        version: 8,
        graph: initial,
        createdAt: "",
        updatedAt: "",
      },
    };
    const get = spyOn(service, "get").mockResolvedValue({
      epoch: 1,
      role: "owner",
      revision: 9,
      cursor: 9,
      events: [{ epoch: 1, revision: 9 }],
    });
    const rendered: CanvasGraphDto[] = [];
    const current: { editor?: ReturnType<typeof useCanvasCollaboration>["editor"] } = {};
    function Workspace() {
      const { editor } = useCanvasCollaboration(snapshot);
      const { connect } = editor;
      useEffect(() => {
        current.editor = editor;
      }, [editor]);
      useEffect(
        () =>
          connect({ read: () => structuredClone(initial), render: (next) => rendered.push(next) }),
        [connect],
      );
      return null;
    }
    const view = await dom.mount(<Workspace />);
    try {
      const editor = current.editor;
      if (!editor) throw new Error("协作未挂载");
      await dom.act(async () => {
        expect(await editor.flush(true)).toBe(true);
      });
      expect(editor.getVersion()).toBe(9);
      expect(rendered).toHaveLength(0);
    } finally {
      await view.unmount();
      get.mockRestore();
      dom.restore();
    }
  });

  test("冲突收据领先一版时追赶后才允许按远端版本应用草稿", () => {
    const initial = graph();
    const session = new CanvasCollaborationSession(1, 8, initial);
    const draft = { kind: "node" as const, id: "first", fields: { "data.label": "我的草稿" } };
    session.capture(initial, applyCollaborationChanges(initial, [draft]));
    const submitted = session.next()!;
    const receipt = {
      epoch: 1,
      opId: submitted.opId,
      revision: 9,
      conflicts: [{ kind: "node", id: "first", field: "data.label", revision: 9, value: "远端" }],
    };
    expect(session.apply(receipt)).toBe(false);
    expect(session.conflicts).toHaveLength(0);
    expect(session.next()).toEqual(submitted);
    expect(
      session.apply({
        epoch: 1,
        opId: "",
        revision: 9,
        changes: [{ ...draft, fields: { "data.label": "远端" } }],
      }),
    ).toBe(true);
    expect(session.apply(receipt)).toBe(true);
    expect(session.optimistic.nodes[0]?.data.label).toBe("远端");
    session.resolve(submitted.opId, true);
    expect(session.next()).toMatchObject({ baseRevision: 9, changes: [draft] });
  });

  test("重新应用并发连线草稿时保留已确认的等价连线和同批其他编辑", () => {
    const initial = graph();
    const session = new CanvasCollaborationSession(1, 8, initial);
    const label = { kind: "node" as const, id: "first", fields: { "data.label": "镜头标题" } };
    const edge = {
      kind: "edge" as const,
      id: "local-edge",
      action: "create" as const,
      value: { source: "first", target: "second", sourceHandle: "out" },
    };
    session.capture(initial, applyCollaborationChanges(initial, [edge, label]));
    const batch = session.next()!;
    session.apply({ epoch: 1, revision: 9, opId: "", changes: [{ ...edge, id: "remote-edge" }] });
    session.apply({
      epoch: 1,
      revision: 9,
      opId: batch.opId,
      conflicts: [{ kind: "edge", id: "remote-edge", field: "", revision: 9, value: null }],
    });
    session.resolve(batch.opId, true);
    expect(session.next()).toMatchObject({ baseRevision: 9, changes: [label] });
    expect(session.optimistic.edges.map(({ id }) => id)).toEqual(["remote-edge"]);
  });

  test("新建和填充节点时只提交素材标识", () => {
    const before = graph();
    const original = before.nodes[0]!;
    if (original.type !== "canvas") throw new Error("缺少素材节点");
    const imported = {
      ...original,
      data: { ...original.data, assetId: "personal-image", src: "/files/private", uploaded: true },
    };
    const after = { ...before, nodes: [imported, before.nodes[1]!, { ...imported, id: "third" }] };
    const changes = diffCollaborationGraph(before, after);
    expect(changes[0]?.fields).toEqual({ "data.assetId": "personal-image" });
    expect(changes[1]?.value?.data).toEqual({
      kind: "image",
      label: "旧标题",
      model: "test",
      assetId: "personal-image",
    });
  });

  test("资产库换图发送新素材 ID，切回已有结果发送结果 ID", () => {
    const before = graph();
    const node = before.nodes[0]!;
    if (node.type !== "canvas") throw new Error("缺少图片节点");
    node.data.assetId = "51";
    node.data.src = "/files/old";
    const replacement = { ...node, data: { ...node.data, assetId: "52", activeOutputId: "52" } };
    expect(
      diffCollaborationGraph(before, { ...before, nodes: [replacement, before.nodes[1]!] })[0]
        ?.fields,
    ).toEqual({ "data.assetId": "52" });
  });

  test("分镜、角色素材和导演台跟随确认版本同步，提交只携带素材 ID", () => {
    const nodes = [
      {
        ...graph().nodes[0]!,
        data: { kind: "storyboard", label: "分镜", storyboard: { shots: [] } },
      },
      {
        ...graph().nodes[0]!,
        id: "role",
        data: {
          kind: "character",
          label: "角色",
          character: {
            name: "甲",
            look: "基础形象",
            description: "",
            extras: [{ assetId: "51", src: "/files/local" }],
            voice: null,
          },
        },
      },
      {
        ...graph().nodes[0]!,
        id: "stage",
        data: {
          kind: "director",
          label: "导演台",
          scene: {
            version: 1,
            duration: 4,
            activeCameraId: "",
            cameras: [],
            props: [],
            characters: [],
            thumb: { assetId: "52", url: "/files/local-thumb" },
          },
        },
      },
    ] as CanvasGraphDto["nodes"];
    const before = { ...graph(), nodes: [] };
    const changes = diffCollaborationGraph(before, { ...before, nodes });
    expect(changes.map((change) => change.value?.data)).toMatchObject([
      { storyboard: { shots: [] } },
      { character: { name: "甲", extras: [{ assetId: "51" }] } },
      { scene: { thumb: { assetId: "52" } } },
    ]);
    expect(JSON.stringify(changes)).not.toContain("/files/");
    const session = new CanvasCollaborationSession(1, 8, before);
    session.capture(before, { ...before, nodes });
    expect(JSON.stringify(session.next())).not.toContain("/files/");
    expect(JSON.stringify(session.optimistic)).toContain("/files/local-thumb");
    const remote = applyCollaborationChanges(before, changes);
    const renamed = { ...nodes[0]!, position: { x: 10, y: 20 } };
    expect(
      diffCollaborationGraph(remote, { ...before, nodes: [renamed, ...nodes.slice(1)] }),
    ).toEqual([{ kind: "node", id: "first", fields: { position: { x: 10, y: 20 } } }]);
  });

  test("换代后仅清理该账户和画布的失效草稿", async () => {
    const dom = await installDom();
    try {
      const outdated = canvasCollaborationDraftKey("4", "c1", 1);
      const current = canvasCollaborationDraftKey("4", "c1", 2);
      const otherAccount = canvasCollaborationDraftKey("5", "c1", 1);
      const otherCanvas = canvasCollaborationDraftKey("4", "c2", 1);
      for (const key of [outdated, current, otherAccount, otherCanvas])
        sessionStorage.setItem(key, "draft");

      pruneCanvasCollaborationDrafts("4", "c1", 2);
      expect(sessionStorage.getItem(outdated)).toBeNull();
      expect(sessionStorage.getItem(current)).toBe("draft");
      pruneCanvasCollaborationDrafts("4", "c1");
      expect(sessionStorage.getItem(current)).toBeNull();
      expect(sessionStorage.getItem(otherAccount)).toBe("draft");
      expect(sessionStorage.getItem(otherCanvas)).toBe("draft");
    } finally {
      dom.restore();
    }
  });

  test("导演台防抖提交保留本地场景开始编辑时的确认版本", () => {
    const begin = {
      version: 1,
      duration: 4,
      activeCameraId: "",
      cameras: [],
      characters: [],
      props: [],
    };
    const local = { ...begin, duration: 6 };
    const remote = { ...begin, duration: 8 };
    const initial = graph();
    const stage = {
      ...initial.nodes[0]!,
      data: { kind: "director", label: "导演台", scene: begin },
    } as CanvasGraphDto["nodes"][number];
    const session = new CanvasCollaborationSession(1, 8, { ...initial, nodes: [stage] });
    const fields = (scene: typeof begin) => [
      { kind: "node" as const, id: stage.id, fields: { "data.scene": scene } },
    ];
    session.apply({ epoch: 1, revision: 9, changes: fields(remote) });
    const before = session.optimistic;
    const after = applyCollaborationChanges(before, fields(local));
    const confirmed = collaborationFieldValue(after.nodes[0]!, "data.scene");
    session.capture(
      before,
      after,
      undefined,
      new Map([[stage.id, { field: "data.scene", revision: 8, value: begin, confirmed }]]),
    );
    expect(session.next()?.changes[0]?.fieldBases).toEqual({ "data.scene": 8 });
  });

  test("组字期间有远端提交时，按开始组字的版本比较提示词", () => {
    const start = graph();
    const session = new CanvasCollaborationSession(1, 8, start);
    const remote = { kind: "node" as const, id: "first", fields: { "data.prompt": "远端" } };
    session.apply({ epoch: 1, revision: 9, changes: [remote] });
    const before = session.optimistic;
    const after = applyCollaborationChanges(before, [
      { ...remote, fields: { "data.prompt": "本地确认" } },
    ]);
    session.capture(
      before,
      after,
      undefined,
      new Map([
        ["first", { field: "data.prompt", revision: 8, value: "起点", confirmed: "本地确认" }],
      ]),
    );
    const submitted = session.next()!;
    expect(submitted.changes[0].fieldBases).toEqual({ "data.prompt": 8 });
    session.apply({
      epoch: 1,
      opId: submitted.opId,
      revision: 9,
      conflicts: [{ kind: "node", id: "first", field: "data.prompt", revision: 9, value: "远端" }],
    });
    expect(session.conflicts[0]?.origins).toEqual({ first: { "data.prompt": "起点" } });
  });

  test("正文弹窗基于打开时的版本提交，保留共同正文用于解决冲突", () => {
    const session = new CanvasCollaborationSession(1, 8, graph());
    const change = {
      kind: "node" as const,
      id: "first",
      fields: { "data.text": "另一成员的正文" },
    };
    session.apply({ epoch: 1, revision: 9, changes: [change] });
    const before = session.optimistic;
    const after = applyCollaborationChanges(before, [
      { ...change, fields: { "data.text": "我的正文" } },
    ]);
    session.capture(
      before,
      after,
      undefined,
      new Map([
        ["first", { field: "data.text", revision: 8, value: "共同起点", confirmed: "我的正文" }],
      ]),
    );
    const submitted = session.next()!;
    expect(submitted.changes[0].fieldBases).toEqual({ "data.text": 8 });
    session.apply({
      epoch: 1,
      opId: submitted.opId,
      revision: 9,
      conflicts: [
        { kind: "node", id: "first", field: "data.text", revision: 9, value: "另一成员的正文" },
      ],
    });
    expect(session.conflicts[0]?.origins).toEqual({ first: { "data.text": "共同起点" } });
  });

  test("只提交主结果标识，并用当前授权历史还原素材", () => {
    const before = graph();
    const first = before.nodes[0]!;
    if (first.type !== "canvas") throw new Error("缺少素材节点");
    Object.assign(first.data, {
      activeOutputId: "512",
      assetId: "512",
      src: "/canvas-media/512",
      outputs: [
        { id: "512", assetId: "512", src: "/canvas-media/512", mediaType: "image", createdAt: 1 },
        { id: "513", assetId: "513", src: "/canvas-media/513", mediaType: "image", createdAt: 2 },
      ],
    });
    const after = structuredClone(before);
    Object.assign(after.nodes[0]!.data, { activeOutputId: "513", src: "/ignored-local-url" });
    const changes = diffCollaborationGraph(before, after);
    expect(changes).toEqual([
      { kind: "node", id: "first", fields: { "data.activeOutputId": "513" } },
    ]);
    expect(applyCollaborationChanges(before, changes).nodes[0]?.data).toMatchObject({
      activeOutputId: "513",
      assetId: "513",
      src: "/canvas-media/513",
    });
  });

  test("短线草稿按各自的创建时间过期，重新入会后需手动应用", async () => {
    const dom = await installDom();
    const now = spyOn(Date, "now").mockReturnValue(1000);
    try {
      const before = graph();
      const after = structuredClone(before);
      after.nodes[0].data.label = "离线草稿";
      const session = new CanvasCollaborationSession(1, 8, before);
      session.capture(before, after);
      session.store("draft");
      now.mockReturnValue(1000 + 25 * 60 * 60 * 1000);
      session.store("draft");
      const resumed = new CanvasCollaborationSession(1, 9, before);
      resumed.restore("draft");
      expect(resumed.next()).toBeNull();
      expect(resumed.optimistic.nodes[0].data.label).toBe("旧标题");
      const [expired] = resumed.conflicts;
      expect(expired?.expired).toBe(true);
      resumed.resolve(expired.batch.opId, true);
      expect(resumed.next()?.baseRevision).toBe(9);
      expect(resumed.optimistic.nodes[0].data.label).toBe("离线草稿");
      now.mockReturnValue(1000 + 8 * 24 * 60 * 60 * 1000);
      session.store("draft");
      expect(sessionStorage.getItem("draft")).toBeNull();
    } finally {
      now.mockRestore();
      dom.restore();
    }
  });

  test("模型参数作为单独的字段组同步，叠加时保留远端提示词", () => {
    const before = graph();
    before.nodes[0].data.params = { prompt: "旧提示词", ratio: "1:1", images: ["27"] };
    const edited = structuredClone(before);
    edited.nodes[0].data.params = { prompt: "旧提示词", ratio: "9:16", images: ["30"] };
    const changes = diffCollaborationGraph(before, edited);
    expect(changes).toEqual([
      {
        id: "first",
        kind: "node",
        fields: {
          "data.params": { ratio: "9:16" },
          "data.manualReferences": { images: [{ assetId: "30" }] },
        },
      },
    ]);
    const remote = structuredClone(before);
    remote.nodes[0].data.params = { prompt: "远端提示词", ratio: "1:1", images: ["27"] };
    expect(applyCollaborationChanges(remote, changes).nodes[0].data.params).toEqual({
      prompt: "远端提示词",
      ratio: "9:16",
      images: ["30"],
    });
  });

  test.each([
    ["提交未确认期间收到的版本通知在同一轮内补取", false, false],
    ["生成使用输入确认的版本，即使同一轮补取了更晚的节点改动", true, false],
    ["编辑成员连续应用无可见改动和后续节点补丁，无需快照", false, true],
  ] as const)("%s", async (_label, generation, hiddenRevision) => {
    const dom = await installDom();
    const start = graph();
    let current = start;
    const { promise: entered, resolve: startCommit } = Promise.withResolvers<CollaborationBatch>();
    const { promise: receipt, resolve: accept } = Promise.withResolvers<CollaborationResult>();
    const post = spyOn(service, "post").mockImplementation((_path, batch) => {
      const submitted = batch as CollaborationBatch;
      if (generation && !submitted.changes[0].fields?.["data.generationInput"]) {
        return Promise.resolve({
          epoch: 1,
          revision: 9,
          opId: submitted.opId,
          changes: submitted.changes,
        }) as ReturnType<typeof service.post>;
      }
      startCommit(submitted);
      return receipt as ReturnType<typeof service.post>;
    });
    const localRevision = generation ? 10 : 9;
    const remoteRevision = localRevision + (hiddenRevision ? 2 : 1);
    const role = hiddenRevision ? "editor" : "owner";
    const get = spyOn(service, "get").mockResolvedValue({
      epoch: 1,
      role,
      revision: remoteRevision,
      cursor: remoteRevision,
      events: [
        ...(hiddenRevision ? [{ epoch: 1, revision: localRevision + 1 }] : []),
        {
          epoch: 1,
          revision: remoteRevision,
          changes: [{ kind: "node", id: "first", fields: { position: { x: 14, y: 20 } } }],
        },
      ],
    });
    let editor: ReturnType<typeof useCanvasCollaboration>["editor"] | undefined;
    const snapshot = {
      epoch: 1,
      role,
      canvas: {
        id: "c1",
        title: "协作",
        description: null,
        version: 8,
        graph: start,
        createdAt: "",
        updatedAt: "",
      },
    };
    function Workspace() {
      const { editor: currentEditor } = useCanvasCollaboration(snapshot);
      const { connect } = currentEditor;
      useEffect(
        () =>
          connect({
            read: () => current,
            render: (next) => {
              current = next;
            },
          }),
        [connect],
      );
      useEffect(() => {
        editor = currentEditor;
      }, [currentEditor]);
      return null;
    }
    const view = await dom.mount(<Workspace />);
    try {
      if (!editor) throw new Error("协作未挂载");
      current = applyCollaborationChanges(start, [
        { kind: "node", id: "first", fields: { "data.label": "编辑中" } },
      ]);
      await dom.act(async () => {
        const flushed = generation
          ? editor?.confirmGeneration("first", "test", { prompt: "确认的提示词" })
          : editor?.flush();
        const batch = await entered;
        dispatchCanvasRoomMessage({
          type: "canvas.collaboration",
          channel: "canvas-collaboration:c1",
          data: { canvasId: "c1", epoch: 1, revision: remoteRevision },
        });
        accept({
          epoch: 1,
          revision: localRevision,
          opId: batch.opId,
          changes: batch.changes,
        });
        expect(await flushed).toBe(generation ? 10 : true);
      });
      expect(get).toHaveBeenCalledTimes(1);
      expect(current.nodes[0].position).toEqual({ x: 14, y: 20 });
      expect(current.nodes[0].data.label).toBe("编辑中");
    } finally {
      await view.unmount();
      post.mockRestore();
      get.mockRestore();
      dom.restore();
    }
  });

  test.each([
    ["页数达到上限", { required: false, largePage: false, pages: 16 }],
    ["服务端要求快照", { required: true, largePage: false, pages: 1 }],
    ["累计增量达到字节预算", { required: false, largePage: true, pages: 1 }],
  ] as const)("重连补快照后才重提草稿（%s）", async (_reason, { required, largePage, pages }) => {
    const dom = await installDom();
    const initial = graph();
    const remoteRevision = 28;
    const edits: CollaborationBatch[] = [];
    const requests: string[] = [];
    const snapshot = {
      epoch: 1,
      role: "owner" as const,
      canvas: {
        id: "c1",
        title: "协作",
        description: null,
        version: 8,
        graph: initial,
        createdAt: "",
        updatedAt: "",
      },
    };
    const moveSecond = (revision: number) => ({
      kind: "node" as const,
      id: "second",
      fields: {
        position: { x: revision, y: 0 },
        ...(largePage ? { "data.prompt": "镜头".repeat(4_000) } : {}),
      },
    });
    let current = initial;
    let confirmed = remoteRevision;
    const get = spyOn(service, "get").mockImplementation(async (path, params) => {
      if (path.endsWith("/events")) {
        requests.push("events");
        const after = Number(params?.after);
        const cursor = required ? after : Math.min(confirmed, after + 1);
        return passResponse({
          epoch: 1,
          role: "owner",
          revision: confirmed,
          cursor,
          events:
            cursor === after ? [] : [{ epoch: 1, revision: cursor, changes: [moveSecond(cursor)] }],
          snapshotRequired: required,
        });
      }
      requests.push("snapshot");
      current = applyCollaborationChanges(current, [
        { kind: "node", id: "first", fields: { position: { x: 57, y: 19 } } },
      ]);
      return passResponse({
        epoch: 1,
        role: "owner",
        canvas: {
          id: "c1",
          title: "协作",
          revision: remoteRevision,
          payload_json: applyCollaborationChanges(initial, [moveSecond(remoteRevision)]),
        },
      });
    });
    const post = spyOn(service, "post").mockImplementation(async (_path, body) => {
      requests.push("commit");
      const batch = body as CollaborationBatch;
      edits.push(batch);
      return passResponse({
        epoch: 1,
        revision: ++confirmed,
        opId: batch.opId,
        changes: batch.changes,
      });
    });
    let flush: ReturnType<typeof useCanvasCollaboration>["editor"]["flush"] | undefined;
    function Workspace() {
      const { editor } = useCanvasCollaboration(snapshot);
      const { connect, flush: flushEditor } = editor;
      useEffect(() => {
        flush = flushEditor;
        return connect({
          read: () => current,
          render: (next) => {
            current = next;
          },
        });
      }, [connect, flushEditor]);
      return null;
    }
    const view = await dom.mount(<Workspace />);
    try {
      current = applyCollaborationChanges(initial, [
        { kind: "node", id: "first", fields: { "data.label": "断线期间的草稿" } },
      ]);
      await dom.act(async () => {
        dispatchCanvasRoomMessage({
          type: "canvas.collaboration",
          channel: "canvas-collaboration:c1",
          data: { canvasId: "c1", epoch: 1, revision: remoteRevision },
        });
        expect(await flush?.()).toBe(true);
      });
      expect(requests.slice(0, requests.indexOf("commit"))).toEqual([
        ...Array<string>(pages).fill("events"),
        "snapshot",
      ]);
      expect(edits.map((batch) => batch.baseRevision)).toEqual([8, 8]);
      expect(edits.flatMap((batch) => batch.changes).map((change) => change.fields)).toEqual([
        { "data.label": "断线期间的草稿" },
        { position: { x: 57, y: 19 } },
      ]);
      expect(current.nodes[0].position).toEqual({ x: 57, y: 19 });
      expect(current.nodes[1].position.x).toBe(remoteRevision);
    } finally {
      await view.unmount();
      post.mockRestore();
      get.mockRestore();
      dom.restore();
    }
  });

  test("服务端拒绝的草稿暂停重试，独立编辑继续同步", async () => {
    const dom = await installDom();
    const initial = graph();
    let current = initial;
    const snapshot = {
      epoch: 1,
      role: "owner" as const,
      canvas: {
        id: "rejected-draft",
        title: "协作",
        description: null,
        version: 8,
        graph: initial,
        createdAt: "",
        updatedAt: "",
      },
    };
    const entered = Promise.withResolvers<void>();
    const invalid = Promise.withResolvers<CollaborationResult>();
    const submissions: CollaborationBatch[] = [];
    const post = spyOn(service, "post").mockImplementation((_path, body) => {
      const batch = body as CollaborationBatch;
      submissions.push(batch);
      entered.resolve();
      if (submissions.length === 1) return invalid.promise as ReturnType<typeof service.post>;
      return Promise.resolve({ epoch: 1, revision: 9, opId: batch.opId, changes: batch.changes });
    });
    const collaboration = { current: null as ReturnType<typeof useCanvasCollaboration> | null };
    function Workspace() {
      const session = useCanvasCollaboration(snapshot);
      const { connect } = session.editor;
      useEffect(() => {
        collaboration.current = session;
      }, [session]);
      useEffect(
        () => connect({ read: () => current, render: (next) => (current = next) }),
        [connect],
      );
      return null;
    }
    const view = await dom.mount(<Workspace />);
    try {
      const first = { kind: "node" as const, id: "first", fields: { "data.label": "待处理" } };
      const dependent = { ...first, fields: { "data.label": "继续编辑草稿" } };
      const independent = {
        kind: "node" as const,
        id: "second",
        fields: { "data.label": "独立修改" },
      };
      await dom.act(async () => {
        current = applyCollaborationChanges(current, [first]);
        const flushing = collaboration.current!.editor.flush();
        await entered.promise;
        current = applyCollaborationChanges(current, [dependent]);
        void collaboration.current!.editor.flush();
        current = applyCollaborationChanges(current, [independent]);
        invalid.reject(new ApiError("协作操作不合法", 10001, 400));
        expect(await flushing).toBe(false);
      });
      expect(submissions.map((batch) => batch.changes)).toEqual([[first], [independent]]);
      expect(current.nodes.map(({ data }) => data.label)).toEqual(["旧标题", "独立修改"]);
      expect(collaboration.current!.editor.status).toBe("conflict");
      expect(
        collaboration.current!.conflicts.map(({ batch, rejected }) => [batch.opId, rejected]),
      ).toEqual([
        [submissions[0].opId, true],
        [expect.any(String), undefined],
      ]);
      expect(collaboration.current!.conflicts[1]?.batch.changes).toEqual([dependent]);

      await dom.act(async () => {
        expect(await collaboration.current!.editor.flush()).toBe(false);
        collaboration.current!.resolve(submissions[0].opId, false);
        expect(await collaboration.current!.editor.flush()).toBe(true);
      });
      expect(submissions).toHaveLength(2);
      expect(collaboration.current!.conflicts).toHaveLength(0);
    } finally {
      await view.unmount();
      post.mockRestore();
      dom.restore();
    }
  });

  test.each(["editor", "viewer"] as const)(
    "重新进入时主动对账并按当前授权恢复普通草稿（%s）",
    async (reconciledRole) => {
      const dom = await installDom();
      const initial = graph();
      const id = "restored-edit";
      const key = canvasCollaborationDraftKey(getCurrentUserId(), id, 1);
      const stored = new CanvasCollaborationSession(1, 8, initial);
      const draft = { kind: "node" as const, id: "first", fields: { "data.label": "待恢复" } };
      stored.capture(initial, applyCollaborationChanges(initial, [draft]));
      const submitted = stored.next()!;
      stored.confirmGeneration("first", "test", { prompt: "已失去权限的生成输入" });
      stored.store(key);
      const replay = Promise.withResolvers<unknown>();
      const get = spyOn(service, "get").mockReturnValue(replay.promise);
      const post = spyOn(service, "post").mockImplementation((_path, body) => {
        const batch = body as CollaborationBatch;
        return Promise.resolve({
          epoch: 1,
          revision: 10,
          opId: batch.opId,
          changes: batch.changes,
        });
      });
      const snapshot = {
        epoch: 1,
        role: "editor" as const,
        canGenerate: false,
        canvas: {
          id,
          title: "协作",
          description: null,
          version: 8,
          graph: initial,
          createdAt: "",
          updatedAt: "",
        },
      };
      function Workspace() {
        const { editor, role } = useCanvasCollaboration(snapshot);
        return <output>{`${role}:${editor.status}:${editor.title}`}</output>;
      }
      const view = await dom.mount(<Workspace />);
      try {
        expect(get).toHaveBeenCalledTimes(1);
        expect(post).not.toHaveBeenCalled();
        expect(view.container.textContent).toBe("editor:saving:协作");
        expect(sessionStorage.getItem(key)).not.toContain("生成输入");
        await dom.act(async () => {
          replay.resolve({
            epoch: 1,
            role: reconciledRole,
            revision: 9,
            cursor: 9,
            title: "CLI 重命名",
            events: [{ epoch: 1, revision: 9, changes: [] }],
          });
        });
        expect(post.mock.calls.map(([, body]) => body)).toEqual(
          reconciledRole === "editor" ? [submitted] : [],
        );
        expect(sessionStorage.getItem(key)).toBeNull();
        if (reconciledRole === "editor")
          expect(view.container.textContent).toBe("editor:saved:CLI 重命名");
      } finally {
        await view.unmount();
        post.mockRestore();
        get.mockRestore();
        dom.restore();
      }
    },
  );

  test.each(["owner", "viewer", "expired"] as const)(
    "个人撤销响应丢失后按当前权限和有效期恢复（%s）",
    async (mode) => {
      const dom = await installDom();
      const snapshot: Parameters<typeof useCanvasCollaboration>[0] = {
        epoch: 1,
        role: "owner",
        canvas: {
          id: "c-undo",
          title: "协作",
          description: null,
          version: 8,
          graph: graph(),
          createdAt: "",
          updatedAt: "",
        },
      };
      const submissions: { epoch: number; opId: string }[] = [];
      const get = spyOn(service, "get").mockResolvedValue({
        epoch: 1,
        role: "owner",
        revision: 8,
        cursor: 8,
        events: [],
      });
      const post = spyOn(service, "post").mockImplementation((_path, body) => {
        const request = body as (typeof submissions)[number];
        submissions.push(request);
        return (
          submissions.length === 1
            ? Promise.reject(new Error("撤销响应丢失"))
            : Promise.resolve({ ...request, revision: 8, undoes: 7 })
        ) as ReturnType<typeof service.post>;
      });
      const startedAt = Date.now();
      const clock = spyOn(Date, "now").mockReturnValue(startedAt);
      let collaboration: ReturnType<typeof useCanvasCollaboration>;
      function Workspace() {
        const current = useCanvasCollaboration(snapshot);
        useEffect(() => {
          collaboration = current;
        }, [current]);
        return null;
      }
      let view = await dom.mount(<Workspace />);
      try {
        await dom.act(async () => {
          expect(collaboration.editor.undo()).toBe(true);
          expect(await collaboration.editor.flush()).toBe(false);
        });
        expect(submissions).toHaveLength(1);
        await view.unmount();
        if (mode === "viewer") snapshot.role = "viewer";
        if (mode === "expired") clock.mockReturnValue(startedAt + 24 * 60 * 60 * 1000);
        view = await dom.mount(<Workspace />);
        if (mode === "owner") {
          expect(submissions).toEqual([submissions[0], submissions[0]]);
          expect(collaboration!.undoResult?.undoes).toBe(7);
        } else {
          expect(submissions).toHaveLength(1);
          expect(collaboration!.undoResult).toBeNull();
        }
        await view.unmount();
        snapshot.role = "owner";
        view = await dom.mount(<Workspace />);
        await dom.act(async () => {
          expect(await collaboration.editor.flush()).toBe(true);
        });
        expect(submissions).toHaveLength(mode === "owner" ? 2 : 1);
      } finally {
        await view.unmount();
        clock.mockRestore();
        post.mockRestore();
        get.mockRestore();
        dom.restore();
      }
    },
  );

  test("仅确认依赖字段的新版本，保留同批独立字段的冲突检查", () => {
    const session = new CanvasCollaborationSession(1, 8, graph());
    const edit = (fields: Record<string, unknown>) => {
      const before = session.optimistic;
      session.capture(
        before,
        applyCollaborationChanges(before, [{ kind: "node", id: "first", fields }]),
      );
    };
    edit({ "data.label": "第一笔标题" });
    const first = session.next()!;
    edit({ "data.label": "第二笔标题", position: { x: 3, y: 5 } });
    expect(session.next()).toEqual(first);
    session.apply({
      epoch: 1,
      opId: "",
      revision: 9,
      changes: [{ kind: "node", id: "first", fields: { position: { x: 11, y: 12 } } }],
    });
    session.apply({ epoch: 1, opId: first.opId, revision: 10, changes: first.changes });
    const second = session.next();
    expect(second?.baseRevision).toBe(8);
    expect(second?.changes[0].fieldBases).toEqual({ "data.label": 10 });
  });

  test.each([8, 9])("删除只沿着已确认的同记录前序操作推进版本（%d）", (previousRevision) => {
    const initial = graph();
    const session = new CanvasCollaborationSession(1, 8, initial);
    const label = { kind: "node" as const, id: "first", fields: { "data.label": "编辑后删除" } };
    session.capture(initial, applyCollaborationChanges(initial, [label]));
    const edit = session.next()!;
    const beforeDelete = session.optimistic;
    session.capture(beforeDelete, { ...beforeDelete, nodes: [] });

    expect(session.apply({ epoch: 1, revision: 9, opId: "" }, "replay")).toBe(true);
    expect(
      session.apply({
        epoch: 1,
        revision: 10,
        opId: edit.opId,
        changes: [{ ...label, previousRevision }],
      }),
    ).toBe(true);
    expect(session.next()).toMatchObject({
      baseRevision: 8,
      changes: [
        {
          kind: "node",
          id: "first",
          action: "delete",
          ...(previousRevision === 8 ? { recordBase: 10 } : {}),
        },
        { kind: "node", id: "second", action: "delete" },
      ],
    });
    expect(Object.hasOwn(session.next()!.changes[0], "recordBase")).toBe(previousRevision === 8);
    expect(Object.hasOwn(session.next()!.changes[1], "recordBase")).toBe(false);
  });

  test("拖动松手后按手势开始的版本检测冲突，保留共同起点", () => {
    const initial = graph();
    const session = new CanvasCollaborationSession(1, 8, initial);
    session.apply({
      epoch: 1,
      opId: "",
      revision: 9,
      changes: [{ kind: "node", id: "first", fields: { position: { x: 11, y: 12 } } }],
    });
    const before = session.optimistic;
    session.capture(
      before,
      applyCollaborationChanges(before, [
        { kind: "node", id: "first", fields: { position: { x: 3, y: 5 } } },
      ]),
      new Map([["first", { revision: 8, position: initial.nodes[0].position, active: false }]]),
    );
    const batch = session.next()!;
    expect(batch.changes[0].fieldBases?.position).toBe(8);
    session.apply({
      epoch: 1,
      opId: batch.opId,
      revision: 9,
      conflicts: [
        { kind: "node", id: "first", field: "position", revision: 9, value: { x: 11, y: 12 } },
      ],
    });
    expect(session.conflicts[0]?.origins?.first?.position).toEqual({ x: 0, y: 0 });
  });

  test("冲突草稿不阻止同一节点的独立位置修改", () => {
    const session = new CanvasCollaborationSession(1, 8, graph());
    const before = session.optimistic;
    session.capture(
      before,
      applyCollaborationChanges(before, [
        { kind: "node", id: "first", fields: { "data.label": "草稿" } },
      ]),
    );
    const first = session.next()!;
    const optimistic = session.optimistic;
    session.capture(
      optimistic,
      applyCollaborationChanges(optimistic, [
        { kind: "node", id: "first", fields: { position: { x: 3, y: 5 } } },
      ]),
    );
    session.apply({
      epoch: 1,
      opId: first.opId,
      revision: 8,
      conflicts: [{ kind: "node", id: "first", field: "data.label", revision: 8, value: "当前" }],
    });
    expect(session.conflicts).toHaveLength(1);
    expect(session.next()?.changes[0].fields).toEqual({ position: { x: 3, y: 5 } });
  });

  test("参数中的提示词与节点提示词共用一次修改", () => {
    const confirmed = graph();
    const draft = structuredClone(confirmed);
    const node = draft.nodes[0];
    if (node.type !== "canvas") throw new Error("需要普通节点");
    node.data.params = { prompt: "新提示词", count: 2 };
    const changes = diffCollaborationGraph(confirmed, draft);
    expect(changes).toEqual([
      {
        kind: "node",
        id: "first",
        fields: { "data.prompt": "新提示词", "data.params": { count: 2 } },
      },
    ]);
    const result = applyCollaborationChanges(confirmed, changes).nodes[0];
    expect(result.data).toMatchObject({
      prompt: "新提示词",
      params: { prompt: "新提示词", count: 2 },
    });
  });
  test("只发送改动的字段，保留远端独立变更、本地视角和未改节点引用", () => {
    const confirmed = graph();
    const draft = structuredClone(confirmed);
    draft.viewport = { x: 200, y: 100, zoom: 2 };
    draft.nodes[0].data.label = "本地标题";
    const changes = diffCollaborationGraph(confirmed, draft);
    expect(changes).toEqual([{ kind: "node", id: "first", fields: { "data.label": "本地标题" } }]);
    const remote = applyCollaborationChanges(confirmed, [
      { kind: "node", id: "first", fields: { position: { x: 12, y: 5 } } },
    ]);
    const merged = applyCollaborationChanges(remote, changes);
    expect(merged.nodes[0].position).toEqual({ x: 12, y: 5 });
    expect(merged.nodes[0].data.label).toBe("本地标题");
    expect(merged.nodes[1]).toBe(confirmed.nodes[1]);
    expect(merged.viewport).toBe(confirmed.viewport);
  });

  test("删除与新建基于稳定记录 ID，忽略服务器持有的任务和媒体字段", () => {
    const confirmed = graph();
    const draft = structuredClone(confirmed);
    const first = draft.nodes[0];
    if (first.type !== "canvas") throw new Error("需要普通节点");
    first.data.status = "running";
    first.data.taskId = "private-task";
    draft.nodes.pop();
    draft.nodes.push({
      id: "third",
      type: "canvas",
      position: { x: 10, y: 30 },
      data: { kind: "script", label: "文本", text: "内容" },
    });
    const changes = diffCollaborationGraph(confirmed, draft);
    expect(changes).toEqual([
      { kind: "node", id: "second", action: "delete" },
      {
        kind: "node",
        id: "third",
        action: "create",
        value: {
          position: { x: 10, y: 30 },
          data: { kind: "script", label: "文本", text: "内容" },
        },
      },
    ]);
    expect(applyCollaborationChanges(confirmed, changes).nodes.map((node) => node.id)).toEqual([
      "first",
      "third",
    ]);
  });
});
