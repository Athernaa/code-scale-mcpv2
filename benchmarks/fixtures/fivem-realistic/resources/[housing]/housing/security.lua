local Security = {}
local Cameras = {}
local Alarms = {}

function AddCamera(propertyId, cameraId, position)
    Cameras[propertyId] = Cameras[propertyId] or {}
    Cameras[propertyId][cameraId] = { id = cameraId, position = position, enabled = true }
    return Cameras[propertyId][cameraId]
end

function SetCameraEnabled(propertyId, cameraId, enabled)
    local camera = Cameras[propertyId] and Cameras[propertyId][cameraId]
    if not camera then
        return false, 'camera_missing'
    end
    camera.enabled = enabled == true
    return true
end

function ListCameras(propertyId)
    return SharedUtils.copy(Cameras[propertyId] or {})
end

function SetAlarm(propertyId, state, reason)
    Alarms[propertyId] = { state = state == true, reason = reason, at = os.time() }
    TriggerEvent('atlas:propertyAlarm', propertyId, Alarms[propertyId])
    return Alarms[propertyId]
end

function GetAlarm(propertyId)
    return Alarms[propertyId] or { state = false }
end

function ClearAlarm(source, propertyId)
    local player = exports.core:GetPlayer(source)
    if not player or not HasKey(propertyId, player.identifier, 'owner') then
        return false, 'not_owner'
    end
    return SetAlarm(propertyId, false, 'cleared')
end

RegisterNetEvent('atlas:propertyAlarmClear')
AddEventHandler('atlas:propertyAlarmClear', function(propertyId)
    ClearAlarm(source, propertyId)
end)

RegisterNetEvent('atlas:propertyCamera')
AddEventHandler('atlas:propertyCamera', function(propertyId, cameraId, enabled)
    SetCameraEnabled(propertyId, cameraId, enabled)
end)

exports('AddCamera', AddCamera)
exports('ListCameras', ListCameras)
exports('SetAlarm', SetAlarm)
exports('ClearAlarm', ClearAlarm)
