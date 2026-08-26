local Reports = {}
local ReportStates = { open = true, investigating = true, resolved = true, rejected = true }

local function newReport(source, category, message)
    return { id = SharedUtils.newId('report', #Reports + 1), source = source, category = category, message = message, state = 'open', createdAt = os.time(), notes = {} }
end

function CreateReport(source, category, message)
    if SharedUtils.isBlank(category) or SharedUtils.isBlank(message) then
        return false, 'report_incomplete'
    end
    local report = newReport(source, category, message)
    Reports[#Reports + 1] = report
    TriggerEvent('atlas:reportCreated', report)
    return true, report
end

function GetReport(reportId)
    for _, report in ipairs(Reports) do
        if report.id == reportId then
            return report
        end
    end
    return nil
end

function SetReportState(source, reportId, state, note)
    if not IsAllowed(source, 'character:inspect') or not ReportStates[state] then
        return false, 'forbidden'
    end
    local report = GetReport(reportId)
    if not report then
        return false, 'report_missing'
    end
    report.state = state
    if note and note ~= '' then
        report.notes[#report.notes + 1] = { source = source, note = note, at = os.time() }
    end
    return true, report
end

function ListReports(state)
    local result = {}
    for _, report in ipairs(Reports) do
        if not state or report.state == state then
            result[#result + 1] = report
        end
    end
    return result
end

RegisterNetEvent('atlas:reportCreate')
AddEventHandler('atlas:reportCreate', function(category, message)
    CreateReport(source, category, message)
end)

RegisterNetEvent('atlas:reportUpdate')
AddEventHandler('atlas:reportUpdate', function(reportId, state, note)
    SetReportState(source, reportId, state, note)
end)

RegisterCommand('admin:reports', function(source)
    if IsAllowed(source, 'character:inspect') then
        TriggerClientEvent('atlas:reportsResult', source, ListReports())
    end
end)

exports('CreateReport', CreateReport)
exports('GetReport', GetReport)
exports('SetReportState', SetReportState)
exports('ListReports', ListReports)
