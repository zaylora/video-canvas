type EdgeSelection = {
  edgeSelected: boolean;
  sourceSelected: boolean;
  targetSelected: boolean;
};

/**
 * 连线和选中的东西有没有关系：选中连线本身，或它连着的任一节点。
 * 有关系的线换成品牌琥珀色，让人一眼看出选中的东西从哪来、往哪去。
 */
export const isEdgeHighlighted = ({
  edgeSelected,
  sourceSelected,
  targetSelected,
}: EdgeSelection) => edgeSelected || sourceSelected || targetSelected;

/**
 * 决定连线上的流光要不要出现：静止时画布只留底线，和选中的东西有关系时才亮起来；
 * 下游正在生成时也亮，提示这根线正在喂数据。
 */
export const shouldShowSweep = ({ running, ...selection }: EdgeSelection & { running: boolean }) =>
  isEdgeHighlighted(selection) || running;
