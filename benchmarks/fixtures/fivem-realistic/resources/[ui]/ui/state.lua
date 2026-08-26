local State = { active = nil, values = {}, listeners = {} }

local function emit(key, value)
    for _, listener in ipairs(State.listeners[key] or {}) do
        listener(value)
    end
    SendNUIMessage({ action = 'state', key = key, value = value })
end

function ReadUIState(key)
    return State.values[key]
end

function WriteUIState(key, value)
    State.values[key] = value
    emit(key, value)
    return value
end

function SubscribeUIState(key, listener)
    if type(listener) ~= 'function' then
        return false
    end
    State.listeners[key] = State.listeners[key] or {}
    State.listeners[key][#State.listeners[key] + 1] = listener
    return true
end

function SetActivePanel(panel)
    State.active = panel
    WriteUIState('activePanel', panel)
end

function GetActivePanel()
    return State.active
end

function ResetUIState()
    State.active = nil
    State.values = {}
    State.listeners = {}
    SendNUIMessage({ action = 'stateReset' })
end

RegisterNUICallback('ui:setState', function(data, callback)
    WriteUIState(data.key, data.value)
    callback({ ok = true })
end)

RegisterNUICallback('ui:getState', function(data, callback)
    callback({ ok = true, value = ReadUIState(data.key) })
end)

exports('ReadUIState', ReadUIState)
exports('WriteUIState', WriteUIState)
exports('SubscribeUIState', SubscribeUIState)
