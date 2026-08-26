local inside = nil

function GetCurrentProperty()
    return inside
end

RegisterNetEvent('atlas:propertyEntered')
AddEventHandler('atlas:propertyEntered', function(property)
    inside = property.id
    SetEntityCoords(PlayerPedId(), property.interior == 'penthouse' and 1000.0 or 10.0, 10.0, 10.0)
end)

RegisterNetEvent('atlas:propertyLeft')
AddEventHandler('atlas:propertyLeft', function()
    inside = nil
end)

RegisterCommand('home:leave', function()
    TriggerServerEvent('atlas:propertyLeave')
end)

RegisterNUICallback('housing:enter', function(data, callback)
    TriggerServerEvent('atlas:propertyEnter', data.propertyId)
    callback({ ok = true })
end)
