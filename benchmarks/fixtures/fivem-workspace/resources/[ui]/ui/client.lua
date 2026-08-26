RegisterNUICallback('inventory:use', function(data, callback)
    TriggerServerEvent('inventory:use', data.item)
    callback({ ok = true })
end)

RegisterNUICallback('vehicle:repair', function(data, callback)
    TriggerServerEvent('vehicle:repair', data.vehicle)
    callback({ ok = true })
end)
