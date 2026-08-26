local Telemetry = {}
local Samples = {}

local function ensureSamples(plate)
    if not Samples[plate] then
        Samples[plate] = {}
    end
    return Samples[plate]
end

function RecordTelemetry(plate, values)
    if not plate or not values then
        return false, 'telemetry_invalid'
    end
    local sample = { speed = values.speed or 0, fuel = values.fuel or 0, damage = values.damage or 0, at = os.time() }
    local samples = ensureSamples(plate)
    samples[#samples + 1] = sample
    while #samples > 50 do
        table.remove(samples, 1)
    end
    return true, sample
end

function LatestTelemetry(plate)
    local samples = ensureSamples(plate)
    return samples[#samples]
end

function AverageTelemetry(plate, field)
    local samples = ensureSamples(plate)
    if #samples == 0 then
        return 0
    end
    local total = 0
    for _, sample in ipairs(samples) do
        total = total + (sample[field] or 0)
    end
    return total / #samples
end

function VehicleHealth(plate)
    local latest = LatestTelemetry(plate)
    if not latest then
        return { fuel = 0, damage = 100, state = 'unknown' }
    end
    local state = latest.damage > 70 and 'critical' or latest.damage > 30 and 'damaged' or 'healthy'
    return { fuel = latest.fuel, damage = latest.damage, state = state }
end

function ClearTelemetry(plate)
    Samples[plate] = nil
    return true
end

RegisterNetEvent('atlas:vehicleTelemetry')
AddEventHandler('atlas:vehicleTelemetry', function(plate, values)
    RecordTelemetry(plate, values)
end)

RegisterCommand('vehicle:health', function(source, _, args)
    TriggerClientEvent('atlas:vehicleHealth', source, args[1], VehicleHealth(args[1]))
end)

exports('RecordTelemetry', RecordTelemetry)
exports('LatestTelemetry', LatestTelemetry)
exports('VehicleHealth', VehicleHealth)
