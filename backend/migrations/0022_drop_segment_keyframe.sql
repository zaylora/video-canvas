-- 片段不再有首帧：片场取消了「生成首帧」，/drama-segments/:sid/keyframe 接口同版本下线，
-- 片段生成视频只按提示词里引用的参考素材走。keyframe_asset_id 只记过某张首帧图的素材 id，
-- 素材本身（assets 表）和已经生成的首帧任务记录都保留，不受影响。
-- 代码已不再读写这一列（model.DramaSegment 同版本去掉 KeyframeAssetID），退回上一版镜像时旧版代码读写这列会报错，
-- 这是「不要首帧」决定的预期后果。
ALTER TABLE IF EXISTS drama_segments DROP COLUMN IF EXISTS keyframe_asset_id;
