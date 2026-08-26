local Training = {}
local Courses = {
    safety = { duration = 15, reward = 50 },
    advanced = { duration = 30, reward = 100 },
    supervisor = { duration = 60, reward = 200 },
}

local function trainingKey(source, course)
    return tostring(source) .. ':' .. course
end

function StartTraining(source, course)
    if not Courses[course] or Training[trainingKey(source, course)] then
        return false, 'training_unavailable'
    end
    Training[trainingKey(source, course)] = { course = course, started = os.time(), progress = 0 }
    TriggerClientEvent('atlas:trainingStarted', source, course, Courses[course].duration)
    return true
end

function AdvanceTraining(source, course, progress)
    local key = trainingKey(source, course)
    local active = Training[key]
    if not active then
        return false, 'training_missing'
    end
    active.progress = SharedUtils.clamp(progress or 0, 0, 100)
    if active.progress >= 100 then
        exports.core:AddCash(source, Courses[course].reward)
        Training[key] = nil
        TriggerClientEvent('atlas:trainingFinished', source, course)
        return true, 'complete'
    end
    return true, active.progress
end

function CancelTraining(source, course)
    local key = trainingKey(source, course)
    if not Training[key] then
        return false, 'training_missing'
    end
    Training[key] = nil
    TriggerClientEvent('atlas:trainingCancelled', source, course)
    return true
end

function GetTraining(source, course)
    return Training[trainingKey(source, course)]
end

RegisterNetEvent('atlas:trainingStart')
AddEventHandler('atlas:trainingStart', function(course)
    StartTraining(source, course)
end)

RegisterNetEvent('atlas:trainingAdvance')
AddEventHandler('atlas:trainingAdvance', function(course, progress)
    AdvanceTraining(source, course, progress)
end)

RegisterNetEvent('atlas:trainingCancel')
AddEventHandler('atlas:trainingCancel', function(course)
    CancelTraining(source, course)
end)

exports('StartTraining', StartTraining)
exports('AdvanceTraining', AdvanceTraining)
exports('GetTraining', GetTraining)
