-- 令牌桶限流
-- KEYS[1] 桶的 key
-- ARGV[1] 生成速率（个/秒）
-- ARGV[2] 桶容量
-- ARGV[3] 当前时间戳（毫秒）
-- 返回 1 放行，0 限流

local key = KEYS[1]
local rate = tonumber(ARGV[1])
local capacity = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

local bucket = redis.call('HMGET', key, 'last_time', 'tokens')
local last_time = tonumber(bucket[1])
local tokens = tonumber(bucket[2])

-- 首次访问：桶按满初始化。
-- 新用户第一次请求就排队是没有道理的，桶的语义本来就是「允许突发」。
if last_time == nil or tokens == nil then
    last_time = now
    tokens = capacity
end

-- 惰性补充：不靠定时任务往桶里加令牌，而是在请求到达时
-- 按时间差一次性算出该补多少，避免维护大量定时器。
local elapsed = math.max(0, now - last_time) / 1000
tokens = math.min(capacity, tokens + elapsed * rate)

if tokens < 1 then
    -- 令牌不足时也要写回：补充后的令牌数是小数值，
    -- 丢掉这次计算结果会让下次重新按老的 last_time 计算，速率就不准了。
    redis.call('HMSET', key, 'last_time', now, 'tokens', tokens)
    redis.call('EXPIRE', key, 120)
    return 0
end

tokens = tokens - 1
redis.call('HMSET', key, 'last_time', now, 'tokens', tokens)
redis.call('EXPIRE', key, 120)
return 1
