local Transactions = {}
local Sequence = 0

local function nextTransaction(source)
    Sequence = Sequence + 1
    return { id = SharedUtils.newId('inventory_tx', Sequence), source = source, changes = {}, state = 'open', at = os.time() }
end

function BeginInventoryTransaction(source)
    if Transactions[source] then
        return false, 'transaction_open'
    end
    Transactions[source] = nextTransaction(source)
    return true, Transactions[source]
end

function AddTransactionChange(source, item, count, direction)
    local transaction = Transactions[source]
    if not transaction or not InventoryConfig.isValidItem(item) or not validateCount(count) then
        return false, 'transaction_invalid'
    end
    transaction.changes[#transaction.changes + 1] = { item = item, count = count, direction = direction }
    return true
end

function CommitInventoryTransaction(source)
    local transaction = Transactions[source]
    if not transaction then
        return false, 'transaction_missing'
    end
    for _, change in ipairs(transaction.changes) do
        local ok
        if change.direction == 'add' then
            ok = AddItem(source, change.item, change.count)
        else
            ok = RemoveItem(source, change.item, change.count)
        end
        if not ok then
            transaction.state = 'rolled_back'
            Transactions[source] = nil
            return false, 'transaction_failed'
        end
    end
    transaction.state = 'committed'
    Transactions[source] = nil
    return true, transaction
end

function RollbackInventoryTransaction(source)
    local transaction = Transactions[source]
    if not transaction then
        return false, 'transaction_missing'
    end
    transaction.state = 'rolled_back'
    Transactions[source] = nil
    return true
end

function GetInventoryTransaction(source)
    return Transactions[source]
end

RegisterNetEvent('atlas:inventoryTransactionBegin')
AddEventHandler('atlas:inventoryTransactionBegin', function()
    BeginInventoryTransaction(source)
end)

RegisterNetEvent('atlas:inventoryTransactionCommit')
AddEventHandler('atlas:inventoryTransactionCommit', function()
    CommitInventoryTransaction(source)
end)

RegisterNetEvent('atlas:inventoryTransactionRollback')
AddEventHandler('atlas:inventoryTransactionRollback', function()
    RollbackInventoryTransaction(source)
end)

exports('BeginInventoryTransaction', BeginInventoryTransaction)
exports('AddTransactionChange', AddTransactionChange)
exports('CommitInventoryTransaction', CommitInventoryTransaction)
