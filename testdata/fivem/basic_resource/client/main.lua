local action = 'save'
TriggerEvent('avenlo:localEvent')

RegisterNUICallback('createCharacter', function(data, cb)
    TriggerServerEvent('avenlo:createCharacter', data)
    cb({ ok = true })
end)

lib.callback.await('avenlo:getCharacter', false, source)
exports.qbx_core:GetPlayer(source)

local dynamicEvent = 'avenlo:' .. action
TriggerServerEvent(dynamicEvent)

-- RegisterNetEvent('avenlo:fakeComment')
local fakeString = "TriggerServerEvent('avenlo:fakeString')"
