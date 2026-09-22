package runcmd

// probeDotnet：根目录恰好一个项目文件（.csproj / .fsproj / .vbproj）→ `dotnet run`。
// 多个项目文件时 dotnet 选不出来；只有解决方案文件（.sln）时项目在子目录里——两种都交给用户。
func probeDotnet(s *scan) outcome {
	projects := s.namesWithExt(".csproj", ".fsproj", ".vbproj")
	switch {
	case len(projects) == 1:
		return s.ok([]string{"dotnet", "run"}, projects[0])
	case len(projects) > 1:
		return problemOutcome(ReasonMultipleEntryPoints, "", projects...)
	}
	if solutions := s.namesWithExt(".sln", ".slnx"); len(solutions) > 0 {
		return problemOutcome(ReasonNoEntryPoint, solutions[0])
	}
	return outcome{}
}
