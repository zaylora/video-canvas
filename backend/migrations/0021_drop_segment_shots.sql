-- 片段不再保存分镜表：分镜步骤只让模型输出每段的视频提示词（镜头、景别、运镜、台词都写在提示词里），
-- 片场也不再展示分镜表。shots 列只存过模型顺带拆出来的结构化镜头，生成时从不提交，删掉不影响任何片段。
-- 代码已不再读写这一列（model.DramaSegment 同版本去掉 Shots），退回上一版镜像时旧版代码读写这列会报错，
-- 这是「不要分镜表」决定的预期后果。
ALTER TABLE IF EXISTS drama_segments DROP COLUMN IF EXISTS shots;
