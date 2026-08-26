local Vehicles = {}
local Reservations = {}

local function normalizePlate(plate)
    return string.upper(string.gsub(plate or '', '%s+', ''))
end

local function ensureVehicle(plate)
    plate = normalizePlate(plate)
    if not Vehicles[plate] then
        Vehicles[plate] = { plate = plate, owner = nil, model = nil, garage = nil, state = 'stored', fuel = 100, damage = 0 }
    end
    return Vehicles[plate]
end

function RegisterVehicle(source, plate, model, garage)
    if not plate or plate == '' then
        return false, 'plate_missing'
    end
    local vehicle = ensureVehicle(plate)
    vehicle.owner = GetPlayer(source) and GetPlayer(source).identifier or nil
    vehicle.model = model
    vehicle.garage = garage
    vehicle.state = 'stored'
    return true, vehicle
end

function GetVehicle(plate)
    return Vehicles[normalizePlate(plate)]
end

function ReserveVehicle(source, plate)
    local vehicle = GetVehicle(plate)
    if not vehicle or vehicle.state ~= 'stored' then
        return false, 'vehicle_unavailable'
    end
    if Reservations[vehicle.plate] and Reservations[vehicle.plate] ~= source then
        return false, 'vehicle_reserved'
    end
    Reservations[vehicle.plate] = source
    return true
end

function SpawnVehicle(source, plate)
    local vehicle = GetVehicle(plate)
    if not vehicle or Reservations[vehicle.plate] ~= source then
        return false, 'reservation_missing'
    end
    vehicle.state = 'out'
    Reservations[vehicle.plate] = nil
    TriggerClientEvent('atlas:vehicleSpawned', source, vehicle)
    return true, vehicle
end

function StoreVehicle(source, plate, fuel, damage)
    local vehicle = GetVehicle(plate)
    if not vehicle then
        return false, 'vehicle_missing'
    end
    vehicle.state = 'stored'
    vehicle.fuel = SharedUtils.clamp(tonumber(fuel) or 0, 0, 100)
    vehicle.damage = SharedUtils.clamp(tonumber(damage) or 0, 0, 100)
    TriggerClientEvent('atlas:vehicleStored', source, vehicle.plate)
    return true
end

function RepairVehicle(source, plate)
    local vehicle = GetVehicle(plate)
    if not vehicle or vehicle.state ~= 'out' then
        return false, 'vehicle_missing'
    end
    if not exports.core:RemoveCash(source, VehicleConfig.repairCost) then
        return false, 'insufficient_funds'
    end
    vehicle.damage = 0
    TriggerClientEvent('atlas:vehicleRepaired', source, vehicle.plate)
    return true
end

RegisterNetEvent('atlas:vehicleStore')
AddEventHandler('atlas:vehicleStore', function(plate, fuel, damage)
    StoreVehicle(source, plate, fuel, damage)
end)

RegisterNetEvent('atlas:vehicleRepair')
AddEventHandler('atlas:vehicleRepair', function(plate)
    RepairVehicle(source, plate)
end)

RegisterCommand('vehicle:repair', function(source, _, args)
    RepairVehicle(source, args[1])
end)

exports('RegisterVehicle', RegisterVehicle)
exports('GetVehicle', GetVehicle)
exports('ReserveVehicle', ReserveVehicle)
exports('SpawnVehicle', SpawnVehicle)
exports('StoreVehicle', StoreVehicle)
exports('RepairVehicle', RepairVehicle)
