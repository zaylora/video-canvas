import { lazy, Suspense } from "react";

import type { CheckoutMode } from "@/api/payment/type";
import { cn } from "@/lib/utils";

/** 二维码库只在收银时才用到，按需加载，不拖慢首屏 */
const QRCodeSVG = lazy(() => import("qrcode.react").then((m) => ({ default: m.QRCodeSVG })));

interface CheckoutQrProps {
  /** 收银形态：qrcode 把 value 画成二维码，qrcode_image 把 value 当图片地址直接显示 */
  mode: Extract<CheckoutMode, "qrcode" | "qrcode_image">;
  /** 二维码内容或图片地址 */
  value: string;
  /** 已过期：盖一层蒙层，二维码不再可用 */
  expired: boolean;
}

const SIZE = 192;

/** 付款二维码。白底留白是扫码识别率的要求，深色主题下也保持白底。 */
export function CheckoutQr({ mode, value, expired }: CheckoutQrProps) {
  return (
    <div
      className="relative mx-auto grid place-items-center rounded-lg bg-white p-2"
      style={{ width: SIZE + 16, height: SIZE + 16 }}
    >
      {mode === "qrcode_image" ? (
        <img src={value} alt="付款二维码" width={SIZE} height={SIZE} className="object-contain" />
      ) : (
        <Suspense fallback={<div className="bg-muted size-48 animate-pulse rounded" />}>
          <QRCodeSVG value={value} size={SIZE} level="M" />
        </Suspense>
      )}
      {expired ? (
        <div
          className={cn(
            "absolute inset-0 grid place-items-center rounded-lg bg-white/92 text-center text-sm text-neutral-900",
          )}
        >
          <div>
            <b>二维码已过期</b>
            <div className="mt-0.5 text-xs text-neutral-600">请重新下单</div>
          </div>
        </div>
      ) : null}
    </div>
  );
}
