{-# LANGUAGE DerivingStrategies #-}
{-# LANGUAGE GeneralizedNewtypeDeriving #-}
{-# LANGUAGE TypeFamilies #-}

module Main (main) where

import Control.Monad (forM)
import Data.List (sort)
import Development.Shake
import Development.Shake.Classes (Binary, Hashable, NFData)
import Development.Shake.FilePath
import System.Directory (createDirectoryIfMissing, getCurrentDirectory)

-- Custom oracle so Go toolchain or target changes invalidate compiled binaries.
newtype GoToolchain = GoToolchain ()
  deriving stock (Show, Eq)
  deriving newtype (Hashable, Binary, NFData)

type instance RuleResult GoToolchain = String

buildDir :: FilePath
buildDir = "_build"

binDir :: FilePath
binDir = buildDir </> "bin"

simDir :: FilePath
simDir = buildDir </> "sim"

main :: IO ()
main = shakeArgs opts $ do
  want ["bins"]

  goToolchain <- addOracle $ \GoToolchain{} -> do
    Stdout out <- cmd "go" ["env", "GOVERSION", "GOOS", "GOARCH"]
    pure (unwords (words out))

  phony "clean" $ do
    putInfo ("Cleaning " ++ buildDir)
    removeFilesAfter buildDir ["//*"]

  phony "bins" $ do
    cmds <- sort <$> getDirectoryDirs "cmd"
    need [binDir </> c | c <- cmds]

  phony "lint" $ do
    cmd_ "go" ["vet", "./..."]
    cmd_ "golangci-lint" ["run", "./..."]

  phony "test" $
    cmd_ (AddEnv "XTESTING_COLOR" "1") "go" ["test", "./..."]

  phony "mutate" $ do
    let allele = binDir </> "allele"
    need [allele]
    base <- getEnvWithDefault "main" "BASE"
    flags <- words <$> getEnvWithDefault "" "FLAGS"
    cmd_ allele (flags ++ ["-strict", "-diff", base, "./..."])

  phony "ci" $
    need ["lint", "test", "mutate"]

  phony "next-version" $
    cmd_ "go" ["run", "./internal/nextversion"]

  phony "measurements" $
    cmd_ "uv" ["run", "marimo", "run", "docs/measurements.py"]

  phony "snapshot" $
    cmd_ "goreleaser" ["release", "--snapshot", "--clean"]

  phony "sim" $
    need [simDir </> "summary.ndjson", simDir </> "strategies.ndjson"]

  -- Each Go binary tracks only its own transitive local Go/embed files via
  -- `go list`, so editing one command never rebuilds unrelated binaries.
  binDir </> "*" %> \out -> do
    _ <- goToolchain (GoToolchain ())
    let pkg = "./cmd/" ++ takeFileName out
    needGoDeps pkg
    liftIO $ createDirectoryIfMissing True (takeDirectory out)
    cmd_ "go" ["build", "-o", out, pkg]

  -- One rule produces both NDJSON files per corpus repo; Shake runs repos in parallel.
  [simDir </> "*" </> "summary.ndjson", simDir </> "*" </> "strategies.ndjson"] &%> \outs -> case outs of
    [sumOut, _stratOut] -> do
      let simBin = binDir </> "simulate"
          repoOutDir = takeDirectory sumOut
          repo = takeFileName repoOutDir
          dataDir = "docs/data" </> repo
      need [simBin]
      seeds <- getEnvWithDefault "5" "SIM_SEEDS"
      inputs <- getDirectoryFiles dataDir ["*.ndjson.gz"]
      need [dataDir </> f | f <- sort inputs]
      liftIO $ createDirectoryIfMissing True repoOutDir
      cmd_ simBin ["-seeds", seeds, "-o", repoOutDir, dataDir]
    _ -> error ("unexpected outputs: " ++ show outs)

  -- Merged NDJSON outputs use writeFileChanged for early cutoff when bytes match.
  simDir </> "summary.ndjson" %> mergeSimFile "summary.ndjson"
  simDir </> "strategies.ndjson" %> mergeSimFile "strategies.ndjson"

opts :: ShakeOptions
opts =
  shakeOptions
    { shakeFiles = buildDir </> "shake"
    , shakeChange = ChangeModtimeAndDigest
    , shakeThreads = 0
    , shakeProgress = progressSimple
    , shakeLint = Just LintBasic
    }

needGoDeps :: String -> Action ()
needGoDeps pkg = do
  let fmt =
        "{{if and .Module .Module.Main}}"
          ++ "{{range .GoFiles}}{{$.Dir}}/{{.}}\n{{end}}"
          ++ "{{range .EmbedFiles}}{{$.Dir}}/{{.}}\n{{end}}"
          ++ "{{end}}"
  Stdout raw <- cmd "go" ["list", "-deps", "-f", fmt, pkg]
  cwd <- liftIO getCurrentDirectory
  let files = [makeRelative cwd p | p <- lines raw, not (null p)]
  need ("go.mod" : "go.sum" : sort files)

mergeSimFile :: FilePath -> FilePath -> Action ()
mergeSimFile name out = do
  repos <- sort <$> getDirectoryDirs "docs/data"
  let parts = [simDir </> r </> name | r <- repos]
  need parts
  chunks <- forM parts readFile'
  writeFileChanged out (concat chunks)
