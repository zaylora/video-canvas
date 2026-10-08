/** 截帧最多等多久（毫秒）：视频解不出来（编码不支持等）时别让上传一直卡着 */
const CAPTURE_TIMEOUT_MS = 8000;

/** 封面的最大宽度（像素）：登录页背景是全屏，1280 够清楚，又不至于把封面传得太大 */
const POSTER_MAX_WIDTH = 1280;

/**
 * 从本地视频文件里截一帧当封面。封面会先于视频显示，也是视频加载失败、省流量、
 * 减少动态效果时的画面，所以后台上传视频时由浏览器截好一起传，后端不需要 ffmpeg。
 * 只能处理本地 File：用跨域地址的视频截帧会污染画布，导不出图片。
 * @param file 视频文件
 * @param atSec 截取的秒数；超过视频时长时取靠近末尾的一帧
 * @returns JPEG 封面文件
 */
export const captureVideoFrame = (file: File, atSec: number): Promise<File> =>
  new Promise((resolve, reject) => {
    const url = URL.createObjectURL(file);
    const video = document.createElement("video");
    video.muted = true;
    video.preload = "auto";
    video.playsInline = true;

    let settled = false;
    const finish = (error: Error | null, result?: File) => {
      if (settled) return;
      settled = true;
      window.clearTimeout(timer);
      URL.revokeObjectURL(url);
      video.removeAttribute("src");
      video.load();
      if (error || !result) reject(error ?? new Error("截取封面失败"));
      else resolve(result);
    };
    const timer = window.setTimeout(() => finish(new Error("截取封面超时")), CAPTURE_TIMEOUT_MS);

    video.addEventListener("error", () => finish(new Error("视频无法解码，没法截取封面")));
    video.addEventListener("loadedmetadata", () => {
      const limit = Number.isFinite(video.duration) ? Math.max(video.duration - 0.1, 0) : atSec;
      video.currentTime = Math.min(Math.max(atSec, 0), limit);
    });
    video.addEventListener("seeked", () => {
      const scale = Math.min(1, POSTER_MAX_WIDTH / (video.videoWidth || POSTER_MAX_WIDTH));
      const canvas = document.createElement("canvas");
      canvas.width = Math.max(1, Math.round(video.videoWidth * scale));
      canvas.height = Math.max(1, Math.round(video.videoHeight * scale));
      const context = canvas.getContext("2d");
      if (!context) {
        finish(new Error("浏览器不支持截取封面"));
        return;
      }
      context.drawImage(video, 0, 0, canvas.width, canvas.height);
      canvas.toBlob(
        (blob) =>
          blob
            ? finish(null, new File([blob], "poster.jpg", { type: "image/jpeg" }))
            : finish(new Error("截取封面失败")),
        "image/jpeg",
        0.82,
      );
    });
    video.src = url;
  });
