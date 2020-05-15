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

    def depends_on(opts)
      name, type = opts.to_a.first

      @info[:dependencies] ||= {}
      @info[:dependencies][type.to_sym] ||= []
      @info[:dependencies][type.to_sym] << name
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

    def head(str=nil, &b)
      if str
        @info[:head] = { url: str }
      else 
        hi = HeadInfo.new
        hi.instance_eval(&b)
        @info[:head] = hi.info
      end
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

class Loader
  def self.load(path, out={}) 
    Kernel.load path

    top = Formula.last_sub

    f = top.info

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
