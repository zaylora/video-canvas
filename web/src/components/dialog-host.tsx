import { Fragment, useEffect } from "react";
import { useLocation } from "react-router";

import { useDialogStore } from "@/store/dialog";

/**
 * 渲染 store 里的全部弹窗，挂在路由根布局里（弹窗里可以用 Link / useNavigate）。
 * 切换页面时把还开着的弹窗全部关掉，避免带到别的页面上。
 */
export function DialogHost() {
  const dialogs = useDialogStore((state) => state.dialogs);
  const close = useDialogStore((state) => state.close);
  const remove = useDialogStore((state) => state.remove);
  const closeAll = useDialogStore((state) => state.closeAll);
  const { pathname } = useLocation();

  useEffect(() => {
    closeAll();
  }, [pathname, closeAll]);

  return dialogs.map((item) => (
    <Fragment key={item.id}>
      {item.render({
        open: item.open,
        onClose: () => close(item.id),
        onExited: () => remove(item.id),
      })}
    </Fragment>
  ));
}
