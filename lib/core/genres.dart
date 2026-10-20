/// The Discover cards: a search to run and the cover art bundled for it.
class Genre {
  final String title;
  final String query;
  final String cover;
  const Genre(this.title, this.query, this.cover);
}

const genres = <Genre>[
  Genre('Top Hits', 'Top Hits', 'assets/covers/top_hits.jpg'),
  Genre('Pop', 'Pop hits', 'assets/covers/pop.jpg'),
  Genre('Rock', 'Rock', 'assets/covers/rock.jpg'),
  Genre('Hip-Hop', 'Hip-Hop', 'assets/covers/hip_hop.jpg'),
  Genre('Lofi & Chill', 'Lofi chill', 'assets/covers/lofi.jpg'),
  Genre('Indie', 'Indie', 'assets/covers/indie.jpg'),
  Genre('Electronic', 'Electronic', 'assets/covers/electronic.jpg'),
  Genre('Jazz & Blues', 'Jazz Blues', 'assets/covers/jazz.jpg'),
  Genre('Acoustic', 'Acoustic', 'assets/covers/acoustic.jpg'),
  Genre('Workout', 'Workout', 'assets/covers/workout.jpg'),
  Genre('Focus & Study', 'Focus study', 'assets/covers/focus.jpg'),
  Genre('Gaming', 'Gaming', 'assets/covers/gaming.jpg'),
];
