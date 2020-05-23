require_relative "./version.rb"

class Formula
  class BottleInfo
    def initialize
      @info = {}
    end

    attr_reader :info

    def cellar(str)
      @info[:cellar] = str
    end

    def rebuild(num)
      @info[:rebuild] = num
    end

    def sha256(opts)
      @info[:releases] ||= {}
      sha, name = opts.to_a.first
      @info[:releases][name] = sha
    end
  end

  class HeadInfo
    def initialize
      @info = {}
    end

    attr_reader :info

    def url(u)
      @info[:url] = u
    end

    def sha256(sha)
      @info[:sha256] = sha
    end

    def patch(x)
    end

    def depends_on(name)
      @info[:dependencies] ||= {}

      if name.kind_of? Hash
        name, type = name.to_a.first

        @info[:dependencies][type.to_sym] ||= []
        @info[:dependencies][type.to_sym] << name
      else
        @info[:dependencies][:runtime] ||= []
        @info[:dependencies][:runtime] << name.to_s
      end
    end
  end

  class << self
    def inherited(kls)
      @last_sub = kls
      kls.info = {}
    end

    attr_reader :last_sub
    attr_accessor :info

    def desc(str)
      @info[:desc] = str
    end

    def homepage(str)
      @info[:homepage] = str
    end

    def url(str)
      @info[:url] = str
    end

    def sha256(str)
      @info[:sha256] = str
    end

    def version(str)
      @info[:version] = str
    end

    def bottle(&b)
      bi = BottleInfo.new
      bi.instance_eval(&b)
      @info[:bottle] = bi.info
    end

    def head(str=nil, opts={}, &b)
      if str
        @info[:head] = { url: str }.merge(opts)
      else 
        hi = HeadInfo.new
        hi.instance_eval(&b)
        @info[:head] = hi.info
      end
    end

    def stable(&b)
      hi = HeadInfo.new
      hi.instance_eval(&b)
      @info[:url] = hi.info[:url]
      @info[:sha256] = hi.info[:sha256]
    end

    def resource(name)
    end

    def mirror(url)
    end

    def revision(num)
    end

    def uses_from_macos(name)
    end

    def version_scheme(num)
    end


    attr_reader :dependencies

    def depends_on(name)
      @info[:dependencies] ||= {}

      if name.kind_of? Hash
        name, type = name.to_a.first

        @info[:dependencies][type.to_sym] ||= []
        @info[:dependencies][type.to_sym] << name
      else
        @info[:dependencies][:runtime] ||= []
        @info[:dependencies][:runtime] << name
      end
    end

    def test(&b)
    end
  end
end

require 'pp'
require 'ripper'

class Loader
  def self.extract_install(path)
    # STDERR.puts path
    sexp = Ripper.sexp(File.read(path))
    class_body = sexp[1][0][3][1]
    install = nil
    class_body.each do |s|
      if s.first == :def || s[0][1] == "install"
        install = s[3][1]
      end
    end

    install.map { |x| translate(x) }
  end

  def self.translate_fn_name(name)
    name.gsub("?", "_eh")
  end

  def self.translate(sexp)
    case sexp.first
    when :body
      sexp[1..-1].map { |x| translate(x) }.join("\n")
    when :call
      "#{translate_call(sexp[1])}.#{translate_fn_name(sexp[3][1])}"
    when :vcall
      sexp[1][1]
    when :if_mod
      "if #{translate(sexp[1])}:\n    #{translate(sexp[2])}"
    when :string_literal
      out = []

      sexp[1][1..-1].each do |s|
        case s.first
        when :@tstring_content
          out << s[1].dump
        else
          out << translate(s)
        end
      end

      out.join(" + ")
    when :string_embexpr
      "str(#{translate(sexp[1][0])})"
    when :command
      name = sexp[1][1]
      args = sexp[2]

      "#{name}#{translate(args)}"
    when :args_add_block
      STDERR.puts sexp.inspect
      parts = sexp[1].map { |x| translate(x) }
      "(#{parts.join(', ')})"
    else
      STDERR.puts sexp.inspect
      raise "unable to handle in call: #{sexp.first}"
    end
  end

  def self.translate_call(sexp)
    case sexp.first
    when :call
      "#{translate_call(sexp[1])}.#{sexp[2][1]}"
    when :vcall
      sexp[1][1]
    else
      raise "unable to handle in call: #{sexp.first}"
    end
  end

  def self.translate_args(args)
    args.map do |a|
      case a[0]
      when :string_literal
        out = []

        # STDERR.puts a[1][1..-1].inspect
        a[1][1..-1].each do |s|
          case s.first
          when :@tstring_content
            out << s[1]
          when :string_embexpr
            if s[1][0].first == :vcall
              out << ("$" + s[1][0][1][1])
            end
          end
        end

        # STDERR.puts a[1].inspect
        # a[1][1][1]
        out.join("").dump
      when :call
        translate_call(a)
      else
        raise "unknown arg type: #{a}"
      end
    end
  end

  def self.translate_install(install)
    out = []

    install.each do |s|
      case s.first
      when :command
        name = s[1][1]
        args = s[2][1]

        out << "#{name}(#{translate_args(args).join(', ')})"
      when :method_add_block
        if s[1].first == :call
          name = s[1][1][1][1]
        end
      when :if_mod
        STDERR.puts s.inspect
        out << "if #{translate_args([s[1]]).first}:\n#{translate_install(s[2])}"
      else
        # STDERR.puts s.inspect
        raise "unknown directive: #{s.first}"
      end
    end

    out
  end

  def self.load(path, out={}) 
    Kernel.load path

    install = Loader.extract_install(path)

    top = Formula.last_sub

    f = top.info

    f[:install] = install

    if b = f[:bottle]
      if r = b[:rebuild]
        f[:rebuild] = r
      end
    end

    f[:name] = top.to_s.downcase

    unless f[:version]
      f[:version] = Version.parse(f[:url]).to_s
    end

    out[f[:name]] = f

    if deps = top.info[:dependencies]
      if runtime = deps[:runtime]
        runtime.each do |dep|
          load File.join(File.dirname(path), dep + ".rb"), out
        end
      end
    end

    out
  end
end
