local OnDuty = {}
local JobState = {}

local function getJob(source)
    local player = exports.core:GetPlayer(source)
    if not player then
        return nil
    end
    return player.job and player.job.name or 'unemployed'
end

function SetDuty(source, state)
    local job = getJob(source)
    if not job or not JobConfig.exists(job) then
        return false, 'job_missing'
    end
    OnDuty[source] = state == true
    JobState[source] = { job = job, started = os.time() }
    TriggerClientEvent('atlas:dutyChanged', source, OnDuty[source], job)
    return true
end

function IsOnDuty(source)
    return OnDuty[source] == true
end

function CompleteShift(source)
    local state = JobState[source]
    if not state or not IsOnDuty(source) then
        return false, 'not_on_duty'
    end
    local config = JobConfig[state.job]
    local minutes = math.max(1, math.floor((os.time() - state.started) / 60))
    local payout = config.salary * minutes
    exports.core:AddCash(source, payout)
    OnDuty[source] = false
    JobState[source] = nil
    TriggerClientEvent('atlas:shiftComplete', source, payout)
    return true, payout
end

function AssignJob(source, job, grade)
    if not JobConfig.exists(job) then
        return false, 'unknown_job'
    end
    return exports.core:UpdateJob(source, job, grade)
end

RegisterNetEvent('atlas:dutyToggle')
AddEventHandler('atlas:dutyToggle', function()
    SetDuty(source, not IsOnDuty(source))
end)

RegisterNetEvent('atlas:shiftComplete')
AddEventHandler('atlas:shiftComplete', function()
    CompleteShift(source)
end)

RegisterCommand('job:duty', function(source)
    SetDuty(source, not IsOnDuty(source))
end)

exports('SetDuty', SetDuty)
exports('CompleteShift', CompleteShift)
exports('AssignJob', AssignJob)
