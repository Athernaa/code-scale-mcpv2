local Missions = {}
local Progress = {}

local Templates = {
    delivery = { reward = 300, steps = { 'collect', 'deliver', 'return' } },
    repair = { reward = 450, steps = { 'inspect', 'repair', 'report' } },
    patrol = { reward = 500, steps = { 'start', 'checkpoint', 'finish' } },
}

local function missionKey(source, id)
    return tostring(source) .. ':' .. tostring(id)
end

function CreateMission(source, kind, target)
    local template = Templates[kind]
    if not template then
        return false, 'mission_missing'
    end
    local id = SharedUtils.newId('mission', #Missions + 1)
    local mission = { id = id, kind = kind, target = target, reward = template.reward, steps = SharedUtils.copy(template.steps), state = 'active' }
    Missions[id] = mission
    Progress[missionKey(source, id)] = { index = 1, started = os.time() }
    TriggerClientEvent('atlas:missionStarted', source, mission)
    return true, mission
end

function GetMission(id)
    return Missions[id]
end

function AdvanceMission(source, id)
    local mission = Missions[id]
    local progress = Progress[missionKey(source, id)]
    if not mission or not progress or mission.state ~= 'active' then
        return false, 'mission_missing'
    end
    progress.index = progress.index + 1
    if progress.index > #mission.steps then
        mission.state = 'complete'
        exports.core:AddCash(source, mission.reward)
        TriggerClientEvent('atlas:missionFinished', source, mission)
        return true, mission
    end
    TriggerClientEvent('atlas:missionStep', source, mission.id, mission.steps[progress.index])
    return true, mission.steps[progress.index]
end

function CancelMission(source, id)
    local mission = Missions[id]
    local key = missionKey(source, id)
    if not mission or not Progress[key] then
        return false, 'mission_missing'
    end
    mission.state = 'cancelled'
    Progress[key] = nil
    TriggerClientEvent('atlas:missionCancelled', source, id)
    return true
end

function ActiveMissions(source)
    local result = {}
    for id, mission in pairs(Missions) do
        if mission.state == 'active' and Progress[missionKey(source, id)] then
            result[#result + 1] = mission
        end
    end
    return result
end

RegisterNetEvent('atlas:missionStart')
AddEventHandler('atlas:missionStart', function(kind, target)
    CreateMission(source, kind, target)
end)

RegisterNetEvent('atlas:missionAdvance')
AddEventHandler('atlas:missionAdvance', function(id)
    AdvanceMission(source, id)
end)

RegisterNetEvent('atlas:missionCancel')
AddEventHandler('atlas:missionCancel', function(id)
    CancelMission(source, id)
end)

exports('CreateMission', CreateMission)
exports('AdvanceMission', AdvanceMission)
exports('ActiveMissions', ActiveMissions)
