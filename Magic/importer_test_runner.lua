-- tts_mock.lua - Tabletop Simulator mock environment
print("Loading Tabletop Simulator mocks...")

self = {
  setName = function(name)
    print("MOCK self.setName: " .. tostring(name))
  end,
  reload = function()
    print("MOCK self.reload called")
  end
}

Wait = {
  time = function(callback, delay)
    callback()
  end
}

spawnObject = function(params)
  return {
    TextTool = {
      setValue = function(val) end,
      setFontSize = function(size) end
    },
    destruct = function() end
  }
end

spawned_cards = {}
spawnObjectData = function(params)
  local data = params.data
  if data.Name == "Deck" then
    print("MOCK spawnObjectData: Spawning Deck of size " .. tostring(#data.ContainedObjects))
    for _, cardDat in ipairs(data.ContainedObjects) do
      table.insert(spawned_cards, cardDat)
    end
  else
    print("MOCK spawnObjectData: Spawning Card " .. tostring(data.Nickname))
    table.insert(spawned_cards, data)
  end
  return {}
end

printToAll = function(msg, color)
  print("MOCK printToAll: " .. tostring(msg))
end
log = function(val, label)
  print("MOCK log: " .. tostring(label) .. " = " .. tostring(val))
end

Vector = function(x, y, z)
  if type(x) == "table" then
    y = x[2] or x.y
    z = x[3] or x.z
    x = x[1] or x.x
  end
  local vec = { x = x or 0, y = y or 0, z = z or 0, [1] = x or 0, [2] = y or 0, [3] = z or 0 }
  setmetatable(vec, {
    __add = function(a, b)
      return Vector(a.x + b.x, a.y + b.y, a.z + b.z)
    end,
    __sub = function(a, b)
      return Vector(a.x - b.x, a.y - b.y, a.z - b.z)
    end,
    __mul = function(a, b)
      if type(b) == "number" then
        return Vector(a.x * b, a.y * b, a.z * b)
      else
        return Vector(a.x * b.x, a.y * b.y, a.z * b.z)
      end
    end,
    __index = {
      add = function(self, other)
        self.x = self.x + other.x
        self.y = self.y + other.y
        self.z = self.z + other.z
        return self
      end,
      set = function(self, x, y, z)
        self.x = x or self.x
        self.y = y or self.y
        self.z = z or self.z
      end
    }
  })
  return vec
end

-- Custom Player mock
last_broadcast = ""
Player = {}
setmetatable(Player, {
  __index = function(t, k)
    return {
      broadcast = function(msg, color)
        print("MOCK Player." .. tostring(k) .. ".broadcast: " .. msg)
        last_broadcast = msg
      end,
      getPointerRotation = function()
        return { 0, 0, 0 }
      end,
      getPointerPosition = function()
        return { 0, 0, 0 }
      end
    }
  end
})

-- Custom JSON parser/encoder for test environment
JSON = {}
function JSON.encode(val)
  if type(val) == 'table' then
    local parts = {}
    local is_array = true
    for k, v in pairs(val) do
      if type(k) ~= 'number' then is_array = false break end
    end
    if is_array then
      for _, v in ipairs(val) do
        table.insert(parts, JSON.encode(v))
      end
      return '[' .. table.concat(parts, ',') .. ']'
    else
      for k, v in pairs(val) do
        table.insert(parts, '"' .. k .. '":' .. JSON.encode(v))
      end
      return '{' .. table.concat(parts, ',') .. '}'
    end
  elseif type(val) == 'string' then
    return '"' .. val:gsub('"', '\\"') .. '"'
  else
    return tostring(val)
  end
end

function JSON.decode(str)
  print("JSON.decode input: " .. tostring(str))
  if not str or str == "" then
    return {}
  end

  local temp_in = "temp_in.json"
  local f = io.open(temp_in, "w")
  f:write(str)
  f:close()

  local py_code = [[
import json
data = json.load(open('temp_in.json', encoding='utf-8'))
def to_lua(val):
    if isinstance(val, dict):
        return "{" + ",".join(f'["{k}"]={to_lua(v)}' for k, v in val.items()) + "}"
    elif isinstance(val, list):
        return "{" + ",".join(to_lua(x) for x in val) + "}"
    elif isinstance(val, str):
        return '"' + val.replace('\\', '\\\\').replace('"', '\\"').replace('\n', '\\n') + '"'
    elif isinstance(val, bool):
        return "true" if val else "false"
    elif val is None:
        return "nil"
    else:
        return str(val)
print(to_lua(data))
]]
  local temp_py = "temp_json.py"
  local f_py = io.open(temp_py, "w")
  f_py:write(py_code)
  f_py:close()

  local p = io.popen("python " .. temp_py)
  local lua_str = p:read("*all")
  p:close()

  os.remove(temp_in)
  os.remove(temp_py)

  -- Parse the Lua table string
  local chunk = loadstring("return " .. lua_str)
  if not chunk then
    return nil
  end
  return chunk()
end

-- Read Proxy URL from command line or default
PROXY_URL = arg[1] or "http://localhost:8000"
print("Configured PROXY_URL: " .. PROXY_URL)

-- Mock WebRequest using curl
WebRequest = {
  post = function(url, body, callback)
    local temp_file = "temp_post.json"
    local f = io.open(temp_file, "w")
    f:write(body)
    f:close()

    local cmd = 'curl -s -X POST -H "Content-Type: application/json" -d @' .. temp_file .. ' "' .. url .. '"'
    print("MOCK WebRequest.post cmd: " .. cmd)
    local p = io.popen(cmd)
    local response = p:read("*all")
    p:close()
    print("MOCK WebRequest.post response: " .. tostring(response))

    os.remove(temp_file)
    callback({ text = response })
  end,
  get = function(url, callback)
    local cmd = 'curl -s "' .. url .. '"'
    local p = io.popen(cmd)
    local response = p:read("*all")
    p:close()

    callback({ text = response })
  end
}

-- Card Metatable Spawning mock is handled inside Importer.lua and spawnObjectData

loop_ended = false
function endLoop()
  print("MOCK endLoop called")
  loop_ended = true
end

-- Load the actual Lua script
print("Loading Importer.lua...")
dofile("Magic/Importer.lua")

-- Test groupUrls helper
print("Testing groupUrls helper...")
local testUrls = {
  "http://localhost:8000/cards/random?q=r:common",
  "http://localhost:8000/cards/random?q=r:common",
  "http://localhost:8000/cards/random?q=r:rare"
}
local grouped = groupUrls(testUrls)
assert(#grouped == 2, "Expected 2 grouped URLs")
assert(grouped[1] == "http://localhost:8000/cards/random?q=r:common&count=2", "Expected count parameter appended")
assert(grouped[2] == "http://localhost:8000/cards/random?q=r:rare", "Expected rare query unchanged")

-- Run E2E Test Case
print("Starting Lua Importer E2E Test Case...")
local testDeck = {
  "https://api.scryfall.com/cards/named?fuzzy=Lightning+Bolt",
  "https://api.scryfall.com/cards/named?fuzzy=NotFoundCard"
}

local qTbl = { color = "white", player = "white", text = function() end }
spawnDeckBatch(testDeck, qTbl)

-- Check assertions
assert(#spawned_cards == 1, "Expected 1 card to spawn successfully")
assert(spawned_cards[1].Nickname:find("Lightning Bolt"), "Expected spawned card to be Lightning Bolt")
assert(last_broadcast:find("Card failed"), "Expected error broadcast for NotFoundCard")

-- Test Random Count E2E
print("Testing Random Count E2E...")
spawned_cards = {}
local rTbl = { color = "white", player = "white", full = "random 3", name = "3", text = function() end }
Importer.Random(rTbl)
assert(#spawned_cards == 3, "Expected 3 random cards to spawn, got " .. #spawned_cards)
for _, card in ipairs(spawned_cards) do
  assert(card.Nickname:find("Random Card"), "Expected card name to be Random Card")
end

-- Test Random Count with Only One Valid Card E2E
print("Testing Random Count with Only One Valid Card E2E...")
spawned_cards = {}
local rTblOne = { color = "white", player = "white", full = "random ?q=OnlyOneValid 2", name = "?q=OnlyOneValid 2", text = function() end }
Importer.Random(rTblOne)
assert(#spawned_cards == 2, "Expected 2 cards to spawn, got " .. #spawned_cards)
for _, card in ipairs(spawned_cards) do
  assert(card.Nickname:find("Only One Valid Card"), "Expected duplicate Only One Valid Card")
end

-- Test collateBoosterPack helper
print("Testing collateBoosterPack helper...")
local cardsCommon = {}
for i=1,5 do table.insert(cardsCommon, {name="Color Common", rarity="common"}) end
for i=1,5 do table.insert(cardsCommon, {name="Generic Common", rarity="common"}) end
for i=1,3 do table.insert(cardsCommon, {name="Standard Uncommon", rarity="uncommon"}) end
table.insert(cardsCommon, {name="Standard Rare", rarity="rare"})
table.insert(cardsCommon, {name="Basic Land", rarity="common"})
table.insert(cardsCommon, {name="Showcase Common", rarity="common"})
local resultCommon = collateBoosterPack(cardsCommon)
assert(#resultCommon == 15, "Collation should reduce pack to 15 cards")
assert(resultCommon[15].name == "Showcase Common", "Showcase Common should be preserved at the end")

local cardsUncommon = {}
for i=1,5 do table.insert(cardsUncommon, {name="Color Common", rarity="common"}) end
for i=1,5 do table.insert(cardsUncommon, {name="Generic Common", rarity="common"}) end
for i=1,3 do table.insert(cardsUncommon, {name="Standard Uncommon", rarity="uncommon"}) end
table.insert(cardsUncommon, {name="Standard Rare", rarity="rare"})
table.insert(cardsUncommon, {name="Basic Land", rarity="common"})
table.insert(cardsUncommon, {name="Showcase Uncommon", rarity="uncommon"})
local resultUncommon = collateBoosterPack(cardsUncommon)
assert(#resultUncommon == 15, "Collation should reduce pack to 15 cards")
assert(resultUncommon[15].name == "Showcase Uncommon", "Showcase Uncommon should be preserved at the end")

local cardsRare = {}
for i=1,5 do table.insert(cardsRare, {name="Color Common", rarity="common"}) end
for i=1,5 do table.insert(cardsRare, {name="Generic Common", rarity="common"}) end
for i=1,3 do table.insert(cardsRare, {name="Standard Uncommon", rarity="uncommon"}) end
table.insert(cardsRare, {name="Standard Rare", rarity="rare"})
table.insert(cardsRare, {name="Basic Land", rarity="common"})
table.insert(cardsRare, {name="Showcase Rare", rarity="rare"})
local resultRare = collateBoosterPack(cardsRare)
assert(#resultRare == 15, "Collation should reduce pack to 15 cards")
assert(resultRare[14].name == "Basic Land", "Rare should be replaced, shifting Basic Land to 14")
assert(resultRare[15].name == "Showcase Rare", "Showcase Rare should be preserved at the end")

-- Test Booster Pack E2E with full pack constraints
print("Testing Booster Pack E2E...")
-- Force math.random to not spawn a showcase randomly for the standard E2E test
local original_random = math.random
math.random = function(a, b)
  if b == 8 then return 2 end -- avoid rolling 1 to bypass showcase
  return original_random(a, b)
end
spawned_cards = {}
local bTbl = { color = "white", player = "white", full = "booster standard", name = "standard", text = function() end }
Importer.Booster(bTbl)
assert(#spawned_cards == 15, "Expected 15 cards spawned in a standard booster pack, got " .. #spawned_cards)

math.random = original_random

local whites, blues, blacks, reds, greens = 0, 0, 0, 0, 0
local commons, uncommons, rares, basics = 0, 0, 0, 0
for _, card in ipairs(spawned_cards) do
  local nick = card.Nickname
  if nick:find("White Common") then whites = whites + 1; commons = commons + 1
  elseif nick:find("Blue Common") then blues = blues + 1; commons = commons + 1
  elseif nick:find("Black Common") then blacks = blacks + 1; commons = commons + 1
  elseif nick:find("Red Common") then reds = reds + 1; commons = commons + 1
  elseif nick:find("Green Common") then greens = greens + 1; commons = commons + 1
  elseif nick:find("Random Card") then commons = commons + 1
  elseif nick:find("Uncommon Card") then uncommons = uncommons + 1
  elseif nick:find("Rare Card") then rares = rares + 1
  elseif nick:find("Basic Land") then basics = basics + 1
  end
end

print("Spawned booster card distribution:")
print("  Commons: " .. commons)
print("    W/U/B/R/G: " .. whites .. "/" .. blues .. "/" .. blacks .. "/" .. reds .. "/" .. greens)
print("  Uncommons: " .. uncommons)
print("  Rares: " .. rares)
print("  Basics: " .. basics)

assert(whites == 1, "Expected exactly 1 white common")
assert(blues == 1, "Expected exactly 1 blue common")
assert(blacks == 1, "Expected exactly 1 black common")
assert(reds == 1, "Expected exactly 1 red common")
assert(greens == 1, "Expected exactly 1 green common")
assert(commons == 10, "Expected exactly 10 commons, got " .. commons)
assert(uncommons == 3, "Expected exactly 3 uncommons")
assert(rares == 1, "Expected exactly 1 rare/mythic")
assert(basics == 1, "Expected exactly 1 basic land, got " .. basics)

-- Test Booster Pack with guaranteed Showcase Card E2E
print("Testing Showcase Booster Pack E2E...")
spawned_cards = {}
local bTblShowcase = { color = "white", player = "white", full = "booster standard_showcase", name = "standard_showcase", text = function() end }
Importer.Booster(bTblShowcase)
assert(#spawned_cards == 15, "Expected 15 cards spawned in showcase booster, got " .. #spawned_cards)

local commons_sc, uncommons_sc, rares_sc, basics_sc = 0, 0, 0, 0
for _, card in ipairs(spawned_cards) do
  local nick = card.Nickname
  if nick:find("Common") or nick:find("Random Card") then commons_sc = commons_sc + 1
  elseif nick:find("Uncommon") then uncommons_sc = uncommons_sc + 1
  elseif nick:find("Rare") then rares_sc = rares_sc + 1
  elseif nick:find("Basic") then basics_sc = basics_sc + 1
  end
end

print("Spawned showcase booster distribution:")
print("  Commons: " .. commons_sc)
print("  Uncommons: " .. uncommons_sc)
print("  Rares: " .. rares_sc)
print("  Basics: " .. basics_sc)

assert(commons_sc == 10, "Expected 10 commons (one replaced by showcase common), got " .. commons_sc)
assert(uncommons_sc == 3, "Expected exactly 3 uncommons, got " .. uncommons_sc)
assert(rares_sc == 1, "Expected exactly 1 rare/mythic, got " .. rares_sc)
assert(basics_sc == 1, "Expected exactly 1 basic land, got " .. basics_sc)

print("\n-------------------------------------------")
print("LUA IMPORTER E2E INTEGRATION TEST PASSED!")
print("-------------------------------------------")
