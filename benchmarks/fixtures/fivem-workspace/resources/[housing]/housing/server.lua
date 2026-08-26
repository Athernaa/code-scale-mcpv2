RegisterNetEvent('housing:enter')
AddEventHandler('housing:enter', function(house)
    return house
end)

exports('GetHouse', function(id)
    return id
end)
