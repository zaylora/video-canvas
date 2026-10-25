-- 画布写入要幂等：崩溃后 Pi 会重放写工具，已经写过的不能再写一遍，而且必须把当初返回给模型的结果原样还原。
--
-- idem_key：调用方显式给的幂等键（Pi 的工具任务 id，每次工具调用唯一、重放时不变）。不能用模型给的 tool_call_id 去重：
-- 有些兼容网关每个回答都从 call_0 开始编号，同一个运行的不同回合会出现相同的 id，那样会悄悄丢掉真实的写入。
-- 旧路径不给键，值是空串，行为不变。
-- result_json：结果里有 tempId 到真实 id 的映射（模型靠它引用刚建的节点）和写入后的提醒，这些以前没有存。
--
-- 两列都带默认值：旧版本的代码不写它们也能正常插入，所以回退到旧镜像仍能用当前的库。
-- 按 (run_id, idem_key) 查已有记录走现有的 (run_id, seq) 唯一索引的前缀，一个运行里的记录很少，不需要新索引；
-- 也不建唯一约束：幂等键为空的行很多，唯一索引还要写成部分索引，没有必要，并发靠画布本来就有的乐观锁串行化。
ALTER TABLE agent_mutations ADD COLUMN idem_key varchar(64) NOT NULL DEFAULT '';
ALTER TABLE agent_mutations ADD COLUMN result_json jsonb NOT NULL DEFAULT '{}';
