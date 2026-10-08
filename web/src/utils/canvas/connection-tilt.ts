type TiltPose = {
  rotateX: number;
  rotateY: number;
  scale: number;
};

/**
 * 节点此刻该用多大的透视距离：倾斜或沉下去时给 perspective，静止时给 0。
 * 透视是 3D 变换，一直挂着会让每个节点都被提升成独立合成层，
 * 画布上节点一多，每帧光合成层计算（Layerize）就要十几毫秒；给 0 时 motion 会把它从 transform 里省掉。
 */
export const tiltPerspective = ({ rotateX, rotateY, scale }: TiltPose, perspective: number) =>
  rotateX === 0 && rotateY === 0 && scale === 1 ? 0 : perspective;
