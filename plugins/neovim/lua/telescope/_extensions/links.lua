-- This is pretty much work in progress and doesn't do anything at the moment
--
-- local actions = require("telescope.actions")
local finders = require("telescope.finders")
local pickers = require("telescope.pickers")
local previewers = require("telescope.previewers")
local sorters = require("telescope.sorters")
-- local action_state = require("telescope.actions.state")
local curl = require("plenary.curl")
local async = require("plenary.async")

local links = {}

local function to_json_string(tbl)
	local result = {}
	for k, v in pairs(tbl) do
		local vv = v
		if v == vim.NIL then
			vv = "null"
		end
		table.insert(result, string.format('"%s": "%s"', k, vv))
	end
	return "{" .. table.concat(result, ", ") .. "}"
end

-- Lazy man's caching
links.__items = {}
links.__prompt = ""

links.get_items = function()
	return function(prompt)
		links.__prompt = prompt
		if prompt == nil then
			prompt = ""
		end
		local has_spaces = string.gmatch(prompt, " ")
		if has_spaces() then
			return links.__items
		end
		prompt, _ = string.gsub(prompt, " ", "%%20")
		-- TODO: move to settings
		local tx, rx = async.control.channel.oneshot()
		curl.get("http://localhost:8090/api/items?q=" .. prompt, {
			callback = vim.schedule_wrap(function(result)
				tx(result)
			end),
		})
		local result = rx()
		local entries = {}
		for i, v in ipairs(vim.json.decode(result.body)) do
			entries[i] = to_json_string(v)
		end
		links.__items = entries
		return entries
	end
end

links.previewer = function()
	return function(self, entry, _)
		-- entry.index
		-- entry.value
		-- entry.display
		-- entry.ordinal
		local lines = {
			"prompt: " .. links.__prompt,
			"display: " .. entry.display,
			"ordinal: " .. entry.ordinal,
			"value: " .. entry.value,
			"index: " .. entry.index,
		}
		vim.api.nvim_buf_set_lines(self.state.bufnr, 0, -1, false, lines)
	end
end

--- Initialize Links Telescope picker extension
---@param opts table: customization options for the picker
local function links_picker(opts)
	-- this is important, because picker buffer number
	-- is different and we need to use to execute commands
	-- in a specific buffer, which is currently open
	-- local bufnr = vim.api.nvim_get_current_buf()

	pickers
		.new(opts, {
			prompt_title = "links",
			finder = finders.new_dynamic({
				fn = links.get_items(),
				entry_maker = function(entry)
					local v = vim.json.decode(entry)
					return {
						value = v.alias,
						display = v.alias .. " " .. v.name .. " " .. v.url,
						ordinal = v.alias,
					}
				end,
			}),
			previewer = previewers.new_buffer_previewer({
				title = "Expansion preview",
				define_preview = links.previewer(),
			}),
			sorter = sorters.get_generic_fuzzy_sorter(),
			-- attach_mappings = function(prompt_bufnr, map)
			-- 	print("chosen")
			-- 	return true
			-- end,
			-- push_cursor_on_edit = true,
		})
		:find()
end

return require("telescope").register_extension({
	setup = function(ext_config, config) end,
	exports = {
		links = function(opts)
			links_picker(opts)
		end,
	},
})
