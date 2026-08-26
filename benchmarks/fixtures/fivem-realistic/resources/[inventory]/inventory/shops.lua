local Shops = {}

local function ensureShop(name)
    if not Shops[name] then
        Shops[name] = { name = name, items = {}, open = true, visitors = {} }
    end
    return Shops[name]
end

function RegisterShop(name, items)
    local shop = ensureShop(name)
    shop.items = SharedUtils.copy(items or {})
    return true, shop
end

function SetShopOpen(name, open)
    local shop = ensureShop(name)
    shop.open = open == true
    return shop.open
end

function GetShop(name)
    return Shops[name]
end

function ListShopItems(name)
    local shop = GetShop(name)
    return shop and SharedUtils.copy(shop.items) or {}
end

function BuyShopItem(source, name, item, count)
    local shop = GetShop(name)
    local entry = shop and shop.items[item]
    if not shop or not shop.open or not entry or not validateCount(count) then
        return false, 'shop_item_unavailable'
    end
    local total = entry.price * count
    if not exports.core:RemoveCash(source, total) then
        return false, 'insufficient_funds'
    end
    local added, addError = AddItem(source, item, count)
    if not added then
        exports.core:AddCash(source, total)
        return false, addError
    end
    Analytics.track('shop_purchase', total, source)
    return true, total
end

function SellShopItem(source, name, item, count)
    local shop = GetShop(name)
    local entry = shop and shop.items[item]
    if not shop or not entry or GetItemCount(source, item) < count then
        return false, 'shop_item_missing'
    end
    local removed, removeError = RemoveItem(source, item, count)
    if not removed then
        return false, removeError
    end
    local total = math.floor(entry.price * count * 0.5)
    exports.core:AddCash(source, total)
    return true, total
end

RegisterNetEvent('atlas:shopBuy')
AddEventHandler('atlas:shopBuy', function(name, item, count)
    BuyShopItem(source, name, item, count or 1)
end)

RegisterNetEvent('atlas:shopSell')
AddEventHandler('atlas:shopSell', function(name, item, count)
    SellShopItem(source, name, item, count or 1)
end)

RegisterCommand('shop:list', function(source, _, args)
    TriggerClientEvent('atlas:shopList', source, args[1], ListShopItems(args[1]))
end)

exports('RegisterShop', RegisterShop)
exports('GetShop', GetShop)
exports('BuyShopItem', BuyShopItem)
exports('SellShopItem', SellShopItem)
