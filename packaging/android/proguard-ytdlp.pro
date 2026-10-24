# youtubedl-android (runs yt-dlp with its bundled Python). It parses JSON
# with Jackson and unpacks archives with commons-compress; keep both from
# being renamed or removed by R8.
-keep class com.yausername.** { *; }
-keep class org.apache.commons.compress.archivers.zip.** { *; }
-keep class com.fasterxml.jackson.** { *; }
-dontwarn org.apache.commons.compress.**
-dontwarn com.fasterxml.jackson.**
-dontwarn java.beans.**
