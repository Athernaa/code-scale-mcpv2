local Payroll = {}
local Payslips = {}

local function payKey(identifier, period)
    return tostring(identifier) .. ':' .. tostring(period)
end

function CalculatePay(source, minutes)
    local player = exports.core:GetPlayer(source)
    local job = player and player.job and player.job.name or 'unemployed'
    local config = JobConfig[job]
    if not config then
        return false, 'job_missing'
    end
    local amount = config.salary * math.max(0, minutes or 0)
    return { job = job, minutes = minutes or 0, amount = amount }
end

function CreatePayslip(source, minutes, period)
    local player = exports.core:GetPlayer(source)
    if not player then
        return false, 'player_missing'
    end
    local calculation, errorMessage = CalculatePay(source, minutes)
    if not calculation then
        return false, errorMessage
    end
    local key = payKey(player.identifier, period)
    if Payslips[key] then
        return false, 'payslip_exists'
    end
    local payslip = SharedUtils.merge(calculation, { identifier = player.identifier, period = period, paid = false })
    Payslips[key] = payslip
    return true, payslip
end

function PayPayslip(source, period)
    local player = exports.core:GetPlayer(source)
    local payslip = player and Payslips[payKey(player.identifier, period)]
    if not payslip or payslip.paid then
        return false, 'payslip_missing'
    end
    exports.core:AddCash(source, payslip.amount)
    payslip.paid = true
    TriggerClientEvent('atlas:payslipPaid', source, payslip)
    return true, payslip
end

function GetPayslip(source, period)
    local player = exports.core:GetPlayer(source)
    return player and Payslips[payKey(player.identifier, period)] or nil
end

function ListPayslips(source)
    local player = exports.core:GetPlayer(source)
    local result = {}
    if not player then
        return result
    end
    for _, payslip in pairs(Payslips) do
        if payslip.identifier == player.identifier then
            result[#result + 1] = payslip
        end
    end
    return result
end

RegisterNetEvent('atlas:payrollCreate')
AddEventHandler('atlas:payrollCreate', function(minutes, period)
    CreatePayslip(source, minutes, period)
end)

RegisterNetEvent('atlas:payrollPay')
AddEventHandler('atlas:payrollPay', function(period)
    PayPayslip(source, period)
end)

exports('CreatePayslip', CreatePayslip)
exports('PayPayslip', PayPayslip)
exports('ListPayslips', ListPayslips)
