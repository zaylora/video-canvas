import type * as Fabric from "fabric";
import type { Canvas, FabricObject, IText, Rect } from "fabric";

import {
  DEFAULT_DOODLE_COLOR,
  DOODLE_WIDTH_DEFAULT,
  createHistoryStack,
  doodleFontSize,
  doodleScale,
  doodleStrokeWidth,
  hasDoodleContent,
  type DoodleItemLike,
  type PixelSize,
} from "./doodle";

/** 动态加载进来的 Fabric 模块；这里只借它的类型，不会把 Fabric 打进主包 */
export type FabricModule = typeof Fabric;

/** 涂鸦工具，三个互斥 */
export type DoodleTool = "pen" | "rect" | "text";

/** 框小于这个边长（逻辑像素）就当手滑，不落框 */
const MIN_RECT_EDGE = 3;

/** 文字用的字体：中文优先用系统黑体，找不到再退到无衬线 */
const TEXT_FONT_FAMILY = '"PingFang SC", "Microsoft YaHei", "Noto Sans CJK SC", sans-serif';

/** 涂鸦引擎：把 Fabric 画布包起来，对外只露出工具、颜色、粗细、撤销重做和导出 */
export type DoodleEngine = {
  /** 切换工具；正在编辑的文字先结束 */
  setTool: (tool: DoodleTool) => void;
  /** 之后新画的东西用这个颜色（已经落笔的不变） */
  setColor: (color: string) => void;
  /** 之后新画的东西用这个粗细（屏幕粗细，引擎换算成逻辑像素） */
  setWidth: (screenWidth: number) => void;
  /** 画布在画布坐标里的显示大小变了（节点被缩放）：更新 CSS 大小和换算比例 */
  setDisplay: (display: PixelSize) => void;
  /** 结束正在编辑的文字（空的移除，非空的锁定并记入撤销栈） */
  commit: () => void;
  /** 撤销一步 */
  undo: () => void;
  /** 重做一步 */
  redo: () => void;
  /** 有没有可撤销的 */
  canUndo: () => boolean;
  /** 有没有可重做的 */
  canRedo: () => boolean;
  /** 有没有任何标注内容（空白文字不算） */
  hasContent: () => boolean;
  /** 导出标注层：和画布逻辑尺寸一样大的透明画布，不含原图 */
  exportLayer: () => HTMLCanvasElement;
  /** 释放 Fabric 画布和它挂的 DOM；之后不能再用 */
  dispose: () => void;
};

/** 造引擎需要的东西 */
export type DoodleEngineInput = {
  /** 动态加载好的 Fabric */
  fabric: FabricModule;
  /** 放画布的空容器；引擎往里建 canvas，dispose 时清空，React 不碰它的子节点 */
  host: HTMLElement;
  /** 画布逻辑尺寸（原图像素，长边超上限时等比缩小） */
  size: PixelSize;
  /** 画布在画布坐标里的显示大小，用来算 逻辑 / 显示 比例 */
  display: PixelSize;
  /** 初始工具 */
  tool: DoodleTool;
  /** 初始颜色 */
  color: string;
  /** 初始粗细（屏幕粗细） */
  width: number;
  /** 撤销栈或重做栈变了：外面据此刷新按钮状态 */
  onChange: () => void;
};

/** 落笔后的对象一律锁定：不能选、不能拖、不能改，只能撤销 */
function lock(object: FabricObject): void {
  object.set({ selectable: false, evented: false });
}

/**
 * 让文字的隐藏输入框聚焦时不滚动页面。Fabric 把它放在 document.body 里、按画布位置摆放，
 * 画布被拖到视口外时聚焦会把整个页面滚走。
 */
function preventFocusScroll(text: IText): void {
  const init = text.initHiddenTextarea.bind(text);
  text.initHiddenTextarea = () => {
    init();
    const input = text.hiddenTextarea;
    if (!input) return;
    const focus = input.focus.bind(input);
    input.focus = (options?: FocusOptions) => focus({ ...options, preventScroll: true });
  };
}

/**
 * 在 host 里建一块 Fabric 画布并接好三个工具。
 * 画布的逻辑尺寸是原图像素，用 CSS 缩小到节点大小显示，所以笔迹坐标、粗细、字号天然是原图像素。
 * 指针坐标由 Fabric 用 getBoundingClientRect 的比例换算（含 CSS 缩小和祖先的 scale 变换），
 * 鼠标事件里统一用 getScenePoint，不自己算。
 * 撤销重做用 createHistoryStack：画布上的对象由它决定增删，只有“落定”的对象才进栈。
 */
export function createDoodleEngine(input: DoodleEngineInput): DoodleEngine {
  const { fabric, host, size, onChange } = input;
  const element = document.createElement("canvas");
  host.appendChild(element);

  const canvas: Canvas = new fabric.Canvas(element, {
    width: size.width,
    height: size.height,
    selection: false,
    preserveObjectStacking: true,
    // 逻辑尺寸已经是原图像素，再乘设备像素比，4096 的图在视网膜屏上会开出 8192² 的画布
    enableRetinaScaling: false,
    stopContextMenu: true,
    fireRightClick: false,
  });

  const stack = createHistoryStack<FabricObject>();
  let tool = input.tool;
  let color = input.color || DEFAULT_DOODLE_COLOR;
  let screenWidth = input.width || DOODLE_WIDTH_DEFAULT;
  let scale = doodleScale(size.width, input.display.width);
  let closed = false;

  /** 正在拖的框；松手时才决定落不落 */
  let draftRect: Rect | null = null;
  let draftOrigin = { x: 0, y: 0 };
  /** 刚点下去新建、等松手再进入编辑的文字（按下时进编辑，焦点会被随后的默认行为抢走） */
  let pendingText: IText | null = null;
  /** 这次按下之前 Fabric 是不是因为点在别处而结束了上一段文字：是就只算“结束”，不再新建 */
  let textEndedByThisDown = false;

  const applyBrush = () => {
    const brush = canvas.freeDrawingBrush;
    if (!brush) return;
    brush.color = color;
    brush.width = doodleStrokeWidth(screenWidth, scale);
  };

  canvas.freeDrawingBrush = new fabric.PencilBrush(canvas);
  canvas.freeDrawingCursor = "crosshair";

  const applyDisplay = (display: PixelSize) => {
    canvas.setDimensions(
      { width: `${display.width}px`, height: `${display.height}px` },
      { cssOnly: true },
    );
    scale = doodleScale(size.width, display.width);
    applyBrush();
  };
  applyDisplay(input.display);

  const setTool = (next: DoodleTool) => {
    // 先结束正在编辑的文字，它会走 editing:exited 落定或移除
    canvas.discardActiveObject();
    tool = next;
    canvas.isDrawingMode = next === "pen";
    canvas.defaultCursor = next === "text" ? "text" : "crosshair";
    canvas.hoverCursor = canvas.defaultCursor;
    applyBrush();
    canvas.requestRenderAll();
  };
  setTool(tool);

  /** 画笔画完：Fabric 已经把路径加进画布，这里锁定并记入栈 */
  canvas.on("path:created", (event) => {
    lock(event.path);
    stack.push(event.path);
    onChange();
  });

  /** 按下的最早时刻（Fabric 取消选中、结束文字编辑之前）：清掉上一次的标记 */
  canvas.on("mouse:down:before", () => {
    textEndedByThisDown = false;
  });

  canvas.on("mouse:down", (event) => {
    if (tool === "rect") {
      const point = canvas.getScenePoint(event.e);
      draftOrigin = { x: point.x, y: point.y };
      draftRect = new fabric.Rect({
        left: point.x,
        top: point.y,
        // Fabric 7 的默认原点是中心，left / top 要当左上角用就得显式写
        originX: "left",
        originY: "top",
        width: 0,
        height: 0,
        // 空心：不填充
        fill: null,
        stroke: color,
        strokeWidth: doodleStrokeWidth(screenWidth, scale),
        // 描边粗细不随对象缩放变化，导出时和屏幕上看到的一致
        strokeUniform: true,
        selectable: false,
        evented: false,
      });
      canvas.add(draftRect);
      return;
    }
    if (tool !== "text") return;
    // 点在正在编辑的文字里：交给 Fabric 移动光标
    if (event.target && (event.target as IText).isEditing) return;
    // 这次点击已经让上一段文字结束编辑了（Fabric 在 mouse:down 之前先取消选中），不再新建
    if (textEndedByThisDown) return;
    const point = canvas.getScenePoint(event.e);
    const text = new fabric.IText("", {
      left: point.x,
      top: point.y,
      originX: "left",
      originY: "top",
      fill: color,
      fontSize: doodleFontSize(screenWidth, scale),
      fontFamily: TEXT_FONT_FAMILY,
      // 编辑期间只要光标和文字，不要选框、手柄，也不能被拖走
      hasControls: false,
      hasBorders: false,
      lockMovementX: true,
      lockMovementY: true,
    });
    text.on("editing:exited", () => {
      if (closed) return;
      textEndedByThisDown = true;
      if (text.text.trim() === "") {
        canvas.remove(text);
      } else {
        lock(text);
        stack.push(text);
      }
      canvas.requestRenderAll();
      onChange();
    });
    preventFocusScroll(text);
    canvas.add(text);
    canvas.setActiveObject(text);
    pendingText = text;
  });

  canvas.on("mouse:move", (event) => {
    if (tool !== "rect" || !draftRect) return;
    const point = canvas.getScenePoint(event.e);
    draftRect.set({
      left: Math.min(point.x, draftOrigin.x),
      top: Math.min(point.y, draftOrigin.y),
      width: Math.abs(point.x - draftOrigin.x),
      height: Math.abs(point.y - draftOrigin.y),
    });
    canvas.requestRenderAll();
  });

  canvas.on("mouse:up", () => {
    if (draftRect) {
      const rect = draftRect;
      draftRect = null;
      if ((rect.width ?? 0) < MIN_RECT_EDGE || (rect.height ?? 0) < MIN_RECT_EDGE) {
        canvas.remove(rect);
      } else {
        rect.setCoords();
        stack.push(rect);
        onChange();
      }
      canvas.requestRenderAll();
    }
    if (pendingText) {
      const text = pendingText;
      pendingText = null;
      text.enterEditing();
    }
  });

  const commit = () => {
    canvas.discardActiveObject();
  };

  return {
    setTool,
    setColor: (next) => {
      color = next;
      applyBrush();
    },
    setWidth: (next) => {
      screenWidth = next;
      applyBrush();
    },
    setDisplay: applyDisplay,
    commit,
    undo: () => {
      commit();
      const item = stack.undo();
      if (item) canvas.remove(item);
      canvas.requestRenderAll();
      onChange();
    },
    redo: () => {
      commit();
      const item = stack.redo();
      if (item) canvas.add(item);
      canvas.requestRenderAll();
      onChange();
    },
    canUndo: stack.canUndo,
    canRedo: stack.canRedo,
    hasContent: () => hasDoodleContent(stack.items() as DoodleItemLike[]),
    exportLayer: () => {
      commit();
      return canvas.toCanvasElement(1);
    },
    dispose: () => {
      if (closed) return;
      commit();
      closed = true;
      // dispose 是异步的，且出错不该影响卸载
      void canvas.dispose().catch(() => undefined);
      host.replaceChildren();
    },
  };
}
