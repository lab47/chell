require 'json'
require_relative "../lib/loader.rb"

info = Loader.load ARGV.shift

puts JSON.dump(info)
