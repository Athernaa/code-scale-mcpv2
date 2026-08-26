local duty = false
local currentJob = 'unemployed'

function IsWorking()
    return duty
end

RegisterNetEvent('atlas:dutyChanged')
AddEventHandler('atlas:dutyChanged', function(state, job)
    duty = state
    currentJob = job
    TriggerEvent('atlas:notify', state and ('On duty: ' .. job) or 'Off duty', state and 'success' or 'info')
end)

RegisterNetEvent('atlas:shiftComplete')
AddEventHandler('atlas:shiftComplete', function(payout)
    TriggerEvent('atlas:notify', 'Shift payout: $' .. tostring(payout), 'success')
end)

RegisterCommand('job:toggle', function()
    TriggerServerEvent('atlas:dutyToggle')
end)

RegisterCommand('job:finish', function()
    TriggerServerEvent('atlas:shiftComplete')
end)

RegisterNUICallback('jobs:toggle', function(_, callback)
    TriggerServerEvent('atlas:dutyToggle')
    callback({ ok = true, job = currentJob })
end)
