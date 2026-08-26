local ready = false
local notifications = {}

local function pushNotification(message, level)
    notifications[#notifications + 1] = { message = message, level = level or 'info', at = GetGameTimer() }
    SendNUIMessage({ action = 'notification', message = message, level = level or 'info' })
end

function IsCoreReady()
    return ready
end

function Notify(message, level)
    pushNotification(message, level)
end

function GetNotifications()
    return notifications
end

RegisterNetEvent('atlas:notify')
AddEventHandler('atlas:notify', function(message, level)
    pushNotification(message, level)
end)

RegisterNetEvent('atlas:sessionReady')
AddEventHandler('atlas:sessionReady', function()
    ready = true
    pushNotification('Session ready', 'success')
end)

CreateThread(function()
    while true do
        Wait(1000)
        if ready then
            TriggerEvent('atlas:clientHeartbeat', GetGameTimer())
        end
    end
end)
