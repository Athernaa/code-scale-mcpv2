local Analytics = {}
local Events = {}

local function eventBucket(name)
    Events[name] = Events[name] or { count = 0, values = {}, last = nil }
    return Events[name]
end

function Analytics.track(name, value, source)
    local bucket = eventBucket(name)
    bucket.count = bucket.count + 1
    bucket.last = { value = value, source = source, at = os.time() }
    if value ~= nil then
        bucket.values[#bucket.values + 1] = tonumber(value) or 0
        while #bucket.values > 100 do
            table.remove(bucket.values, 1)
        end
    end
    return true
end

function Analytics.count(name)
    return eventBucket(name).count
end

function Analytics.average(name)
    local values = eventBucket(name).values
    if #values == 0 then
        return 0
    end
    local total = 0
    for _, value in ipairs(values) do
        total = total + value
    end
    return total / #values
end

function Analytics.last(name)
    return eventBucket(name).last
end

function Analytics.snapshot()
    return SharedUtils.copy(Events)
end

function Analytics.reset(name)
    if name then
        Events[name] = nil
    else
        Events = {}
    end
    return true
end

RegisterNetEvent('atlas:track')
AddEventHandler('atlas:track', function(name, value)
    Analytics.track(name, value, source)
end)

RegisterCommand('atlas:metrics', function(source)
    TriggerClientEvent('atlas:metricsResult', source, Analytics.snapshot())
end)

exports('TrackMetric', Analytics.track)
exports('MetricCount', Analytics.count)
exports('MetricSnapshot', Analytics.snapshot)
