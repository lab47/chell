require 'ripper'

Dir[ARGV.shift].each do |path|
  # STDERR.puts path
  sexp = Ripper.sexp(File.read(path))
  class_body = sexp[1].find { |x| x.first == :class}[3][1]

  install = nil
  class_body.each do |s|
    if s.first == :def
      if s[1][1] == "install"
        install = s[3][1]
      end
    end
  end

  if install

    handler = ->(s) {
      case s.first
      when :command
        puts "command:#{s[1][1]}"
      when :method_add_block
        puts "block_command:#{s[1][1][1][1]}"
      when :string_literal
        puts "string"
      when :command_call
        puts "ccall:#{s[1][1][1]}"
      when :call
        puts "call:#{s[1][1][1]}"
      when :assign
        puts "assign:#{s[1][1][1]}"
        # STDERR.puts "assign:#{s.inspect}"
        handler.(s[2])
      when :vcall
        puts "vcall:#{s[1][1]}"
      when :fcall
        puts "fcall:#{s[1][1]}"
      when :binary
        puts "binary:#{s[2]}"
        handler.(s[1])
        handler.(s[3])
      when :method_add_arg
        handler.(s[1])
      when :array
        puts "array"
      when :var_ref
        puts "var:#{s[1][1]}"
      when :symbol_literal
        puts "symbol:#{s[1][1][1]}"
      when :@int
        puts "int"
      when :aref
        puts "aref"
        handler.(s[1])
      when :if
        puts "if"
        handler.(s[1])
        s[2].each { |x| handler.(x) }
        handler.(s[3]) if s[3]
      when :elsif
        puts "elsif"
        handler.(s[1])
        s[2].each { |x| handler.(x) }
        handler.(s[3]) if s[3]
      when :unless
        puts "unless"
        handler.(s[1])
        s[2].each { |x| handler.(x) }
        handler.(s[3]) if s[3]
      when :else
        s[1].each { |x| handler.(x) }
      when :if_mod
        puts "if"
        handler.(s[1])
        handler.(s[2])
      when :unless_mod
        puts "unless"
        handler.(s[1])
        handler.(s[2])
      else
        # STDERR.puts s.inspect
      end
    }

    install.each do |s|
      handler.(s)
    end
  end
end
