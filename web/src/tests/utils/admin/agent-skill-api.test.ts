import { describe, expect, test } from "bun:test";

import { buildImportForm } from "@/api/admin/agent-skill";
import { adminAgentSkillEndpoints as ep } from "@/api/admin/agent-skill/endpoints";

describe("导入请求体", () => {
  test("zip 只带 file 字段", () => {
    const zip = new File(["x"], "a.zip");
    const form = buildImportForm({ kind: "zip", file: zip });
    expect([...form.keys()]).toEqual(["file"]);
    expect((form.get("file") as File).name).toBe("a.zip");
  });

  test("文件夹的 files 与 paths 逐文件交替追加，下标一一对应，路径含外层目录", () => {
    const form = buildImportForm({
      kind: "files",
      entries: [
        { file: new File(["a"], "SKILL.md"), path: "demo/SKILL.md" },
        { file: new File(["b"], "tone.md"), path: "demo/references/tone.md" },
      ],
    });
    expect([...form.keys()]).toEqual(["files", "paths", "files", "paths"]);
    expect(form.getAll("paths")).toEqual(["demo/SKILL.md", "demo/references/tone.md"]);
    expect(form.getAll("files").map((f) => (f as File).name)).toEqual(["SKILL.md", "tone.md"]);
  });
});

describe("接口路径", () => {
  test("技能名和路径参数会被编码", () => {
    expect(ep.detail("a b")).toBe("/admin/agent/skills/a%20b");
    expect(ep.versionFile("x", 2)).toBe("/admin/agent/skills/x/versions/2/files");
    expect(ep.versionDownload("x", 2)).toBe("/admin/agent/skills/x/versions/2/download");
    expect(ep.importConfirm("id1")).toBe("/admin/agent/skills/imports/id1/confirm");
  });
});
