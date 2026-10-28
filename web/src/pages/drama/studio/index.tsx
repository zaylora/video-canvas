import { Navigate, useParams } from "react-router";

/**
 * 旧地址 /drama/:id/ep/:no：片场已并入工作台，跳到 /drama/:id?area=studio&ep=:no，
 * 书签和分享出去的链接仍然有效。
 */
export default function DramaStudioRedirect() {
  const { id = "", no = "1" } = useParams();
  return <Navigate to={`/drama/${id}?area=studio&ep=${no}`} replace />;
}
