local Listings = {}
local Offers = {}

local function listingId(propertyId)
    return 'listing:' .. tostring(propertyId)
end

function ListProperty(source, propertyId, price)
    if not price or price <= 0 then
        return false, 'price_invalid'
    end
    local property = Properties[propertyId]
    local player = exports.core:GetPlayer(source)
    if not property or not player or property.owner ~= player.identifier then
        return false, 'not_owner'
    end
    local listing = { id = listingId(propertyId), propertyId = propertyId, owner = player.identifier, price = price, state = 'listed', at = os.time() }
    Listings[listing.id] = listing
    return true, listing
end

function GetListing(propertyId)
    return Listings[listingId(propertyId)]
end

function RemoveListing(source, propertyId)
    local listing = GetListing(propertyId)
    local player = exports.core:GetPlayer(source)
    if not listing or not player or listing.owner ~= player.identifier then
        return false, 'listing_missing'
    end
    Listings[listing.id] = nil
    return true
end

function MakeOffer(source, propertyId, amount)
    local listing = GetListing(propertyId)
    local player = exports.core:GetPlayer(source)
    if not listing or not player or amount < listing.price then
        return false, 'offer_invalid'
    end
    local offer = { id = SharedUtils.newId('offer', #Offers + 1), listing = listing.id, buyer = player.identifier, amount = amount, state = 'open' }
    Offers[#Offers + 1] = offer
    TriggerEvent('atlas:propertyOffer', offer)
    return true, offer
end

function AcceptOffer(source, offerId)
    for _, offer in ipairs(Offers) do
        local listing = Listings[offer.listing]
        local player = exports.core:GetPlayer(source)
        if offer.id == offerId and listing and player and listing.owner == player.identifier then
            offer.state = 'accepted'
            listing.state = 'sold'
            return true, offer
        end
    end
    return false, 'offer_missing'
end

function ListListings()
    local result = {}
    for _, listing in pairs(Listings) do
        if listing.state == 'listed' then
            result[#result + 1] = listing
        end
    end
    return result
end

RegisterNetEvent('atlas:propertyList')
AddEventHandler('atlas:propertyList', function(propertyId, price)
    ListProperty(source, propertyId, price)
end)

RegisterNetEvent('atlas:propertyOffer')
AddEventHandler('atlas:propertyOffer', function(propertyId, amount)
    MakeOffer(source, propertyId, amount)
end)

exports('ListProperty', ListProperty)
exports('GetListing', GetListing)
exports('MakeOffer', MakeOffer)
exports('ListListings', ListListings)
