VidAgent
========

VidAgent is a wrapper over ffmpeg that makes it easy to filter your own videos and movies for content.

_Still a WIP! Changes may be breaking for now._

You provide an input file, and output filename, and a filter file. Filter files specify actions to perform on the video and for which segments. Currently, the following actions are supported:

- **cut** splices out a segment of video and audio as if it was never there
- **mute** mutes the audio for a segment but leaves the image intact


## Requirements

This program requires [ffmpeg](https://www.ffmpeg.org/) to be installed with its command in your PATH.

On Mac, `brew install ffmpeg` will do. For Ubuntu, `apt install ffmpeg` works. On Windows, [download ffmpeg](https://www.ffmpeg.org/download.html) from its website.


## Install

```
go get github.com/mholt/vidagent/cmd/vidagent
```

VidAgent is pure Go, so it cross-compiles easily. `ffmpeg` is NOT required for compilation.


## Usage

Once you have the video file you want to edit, create a filter file to specify which actions you would like to perform. They're easy to write by hand. For example:

```
cut 1:32-1:45
mute 2:19.2-2:19.85
```

This filter file removes everything between 1:32 and 1:45 (Minute:Second), then mutes everything (presumably a word, in this case) from 2:19.2 to 2:19.85 (Minute:Second.Fraction). Then run the command:

```
vidagent -filter example.filter -in input_video.mp4 -out output_video.mp4
```

You can force overwriting an existing output file with `-f`.

Although VidAgent is merely a wrapper for the ffmpeg command, the resulting ffmpeg command is too unwieldy to create by hand, especially over an entire video collection. VidAgent abstracts that away so it's easy to run this on lots of videos.
