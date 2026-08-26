local Dispatch = {}
local Calls = {}
local Subscribers = {}

local function nextCallId()
    return SharedUtils.newId('call', #Calls + 1)
end

function Dispatch.subscribe(source, job)
    Subscribers[source] = job
    return true
end

function Dispatch.unsubscribe(source)
    Subscribers[source] = nil
end

function Dispatch.create(source, job, location, message)
    if not JobConfig.exists(job) then
        return false, 'job_missing'
    end
    local call = { id = nextCallId(), source = source, job = job, location = location, message = message, state = 'open', responders = {} }
    Calls[#Calls + 1] = call
    for target, subscribedJob in pairs(Subscribers) do
        if subscribedJob == job and IsOnDuty(target) then
            TriggerClientEvent('atlas:dispatchCall', target, call)
        end
    end
    return true, call
end

function Dispatch.respond(source, callId)
    for _, call in ipairs(Calls) do
        if call.id == callId and call.state == 'open' then
            call.responders[source] = os.time()
            call.state = 'assigned'
            TriggerClientEvent('atlas:dispatchAssigned', call.source, call.id, source)
            return true, call
        end
    end
    return false, 'call_missing'
end

function Dispatch.close(source, callId)
    for _, call in ipairs(Calls) do
        if call.id == callId and call.responders[source] then
            call.state = 'closed'
            call.closedBy = source
            TriggerClientEvent('atlas:dispatchClosed', call.source, call.id)
            return true
        end
    end
    return false, 'call_missing'
end

function Dispatch.list(job)
    local result = {}
    for _, call in ipairs(Calls) do
        if call.job == job and call.state ~= 'closed' then
            result[#result + 1] = call
        end
    end
    return result
end

RegisterNetEvent('atlas:dispatchSubscribe')
AddEventHandler('atlas:dispatchSubscribe', function(job)
    Dispatch.subscribe(source, job)
end)

RegisterNetEvent('atlas:dispatchCreate')
AddEventHandler('atlas:dispatchCreate', function(job, location, message)
    Dispatch.create(source, job, location, message)
end)

RegisterNetEvent('atlas:dispatchRespond')
AddEventHandler('atlas:dispatchRespond', function(callId)
    Dispatch.respond(source, callId)
end)

exports('CreateDispatch', Dispatch.create)
exports('RespondDispatch', Dispatch.respond)
exports('ListDispatch', Dispatch.list)
